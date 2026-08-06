package trigger

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/imprun/windforce-core/internal/state"
	amqp "github.com/rabbitmq/amqp091-go"
)

type RabbitMQConfig struct {
	Queue            string `json:"queue"`
	Prefetch         int    `json:"prefetch,omitempty"`
	Concurrency      int    `json:"concurrency,omitempty"`
	ConsumerTag      string `json:"consumer_tag,omitempty"`
	DeliveryIDHeader string `json:"delivery_id_header,omitempty"`
	InputMode        string `json:"input_mode,omitempty"`
}

type rabbitMQSecret struct {
	URL string `json:"url"`
}

type RabbitMQTrigger struct {
	definition state.TriggerDefinition
	config     RabbitMQConfig
	url        string
	submitter  *Submitter

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewRabbitMQTrigger(definition state.TriggerDefinition) (Trigger, error) {
	config, url, err := parseRabbitMQDefinition(definition)
	if err != nil {
		return nil, err
	}
	return &RabbitMQTrigger{definition: definition, config: config, url: url}, nil
}

func (t *RabbitMQTrigger) Initialize(runtime Runtime) error {
	if runtime.Submitter == nil {
		return errors.New("RabbitMQ trigger submitter is required")
	}
	t.submitter = runtime.Submitter
	return nil
}

func (t *RabbitMQTrigger) Start(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cancel != nil {
		return nil
	}
	if t.submitter == nil {
		return errors.New("RabbitMQ trigger is not initialized")
	}
	runCtx, cancel := context.WithCancel(ctx)
	t.cancel = cancel
	t.done = make(chan struct{})
	go t.run(runCtx, t.done)
	return nil
}

func (t *RabbitMQTrigger) Stop(ctx context.Context) error {
	t.mu.Lock()
	cancel := t.cancel
	done := t.done
	t.cancel = nil
	t.done = nil
	t.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *RabbitMQTrigger) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	backoff := time.Second
	for ctx.Err() == nil {
		err := t.consume(ctx)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			backoff = time.Second
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (t *RabbitMQTrigger) consume(ctx context.Context) error {
	connection, err := amqp.DialConfig(t.url, amqp.Config{
		Dial: (&net.Dialer{Timeout: 10 * time.Second}).Dial,
	})
	if err != nil {
		return err
	}
	defer connection.Close()

	channel, err := connection.Channel()
	if err != nil {
		return err
	}
	defer channel.Close()
	if err := channel.Qos(t.config.Prefetch, 0, false); err != nil {
		return err
	}
	deliveries, err := channel.ConsumeWithContext(
		ctx,
		t.config.Queue,
		t.config.ConsumerTag,
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	var workers sync.WaitGroup
	workers.Add(t.config.Concurrency)
	for range t.config.Concurrency {
		go func() {
			defer workers.Done()
			for delivery := range deliveries {
				deliveryCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				t.handleDelivery(deliveryCtx, delivery)
				cancel()
			}
		}()
	}
	workers.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return errors.New("RabbitMQ delivery stream closed")
}

func (t *RabbitMQTrigger) handleDelivery(ctx context.Context, delivery amqp.Delivery) {
	deliveryID := strings.TrimSpace(delivery.MessageId)
	if t.config.DeliveryIDHeader != "" {
		if value, ok := delivery.Headers[t.config.DeliveryIDHeader]; ok {
			if headerID := strings.TrimSpace(fmt.Sprint(value)); headerID != "" {
				deliveryID = headerID
			}
		}
	}
	if deliveryID == "" {
		sum := sha256.Sum256(delivery.Body)
		deliveryID = "body-" + hex.EncodeToString(sum[:])
	}

	input := json.RawMessage(delivery.Body)
	if t.config.InputMode == "raw" {
		input, _ = json.Marshal(map[string]string{
			"raw_base64":   base64.StdEncoding.EncodeToString(delivery.Body),
			"content_type": delivery.ContentType,
		})
	}
	submission := t.submitter.Submit(ctx, t.definition, Event{
		TriggerID:     t.definition.ID,
		DeliveryID:    deliveryID,
		CorrelationID: delivery.CorrelationId,
		Input:         input,
		RawPayload:    append([]byte(nil), delivery.Body...),
		SafeMetadata:  safeRabbitMQMetadata(delivery),
	})
	switch submission.State {
	case DeliveryAdmitted:
		_ = delivery.Ack(false)
	case DeliveryRetryable:
		_ = delivery.Nack(false, true)
	default:
		_ = delivery.Reject(false)
	}
}

func parseRabbitMQDefinition(definition state.TriggerDefinition) (RabbitMQConfig, string, error) {
	var config RabbitMQConfig
	if err := json.Unmarshal(definition.Config, &config); err != nil {
		return RabbitMQConfig{}, "", fmt.Errorf("RabbitMQ config: %w", err)
	}
	config.Queue = strings.TrimSpace(config.Queue)
	if config.Queue == "" {
		return RabbitMQConfig{}, "", errors.New("RabbitMQ queue is required")
	}
	if config.Concurrency == 0 {
		config.Concurrency = 1
	}
	if config.Concurrency < 1 || config.Concurrency > 128 {
		return RabbitMQConfig{}, "", errors.New("RabbitMQ concurrency must be between 1 and 128")
	}
	if config.Prefetch == 0 {
		config.Prefetch = config.Concurrency
	}
	if config.Prefetch < config.Concurrency || config.Prefetch > 65535 {
		return RabbitMQConfig{}, "", errors.New("RabbitMQ prefetch must be at least concurrency and no more than 65535")
	}
	if config.ConsumerTag == "" {
		config.ConsumerTag = "wf-trigger-" + definition.ID
	}
	if config.InputMode == "" {
		config.InputMode = "json"
	}
	if config.InputMode != "json" && config.InputMode != "raw" {
		return RabbitMQConfig{}, "", errors.New("RabbitMQ input_mode must be json or raw")
	}
	var secret rabbitMQSecret
	if err := json.Unmarshal(definition.SecretConfig, &secret); err != nil {
		return RabbitMQConfig{}, "", fmt.Errorf("RabbitMQ secret config: %w", err)
	}
	secret.URL = strings.TrimSpace(secret.URL)
	if secret.URL == "" {
		return RabbitMQConfig{}, "", errors.New("RabbitMQ URL is required")
	}
	return config, secret.URL, nil
}

func safeRabbitMQMetadata(delivery amqp.Delivery) map[string]string {
	result := map[string]string{
		"content_type":   delivery.ContentType,
		"exchange":       delivery.Exchange,
		"routing_key":    delivery.RoutingKey,
		"redelivered":    strconv.FormatBool(delivery.Redelivered),
		"message_id":     delivery.MessageId,
		"correlation_id": delivery.CorrelationId,
	}
	for _, name := range []string{"traceparent", "tracestate"} {
		for key, value := range delivery.Headers {
			if strings.EqualFold(strings.TrimSpace(key), name) {
				result[name] = strings.TrimSpace(fmt.Sprint(value))
				break
			}
		}
	}
	for key, value := range result {
		value = strings.TrimSpace(value)
		if value == "" {
			delete(result, key)
			continue
		}
		if len(value) > 1024 {
			result[key] = value[:1024]
		}
	}
	return result
}
