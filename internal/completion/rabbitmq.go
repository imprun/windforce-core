package completion

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rabbitmq/amqp091-go"

	"github.com/imprun/windforce-core/internal/state"
	"github.com/imprun/windforce-core/internal/webhook"
)

type RabbitMQSender struct {
	RequestTimeout time.Duration
}

func (sender RabbitMQSender) Send(ctx context.Context, claim *state.TriggerCompletionClaim) webhook.AttemptResult {
	started := time.Now()
	finish := func(result webhook.AttemptResult) webhook.AttemptResult {
		result.Latency = time.Since(started)
		return result
	}
	if claim == nil || claim.Delivery.Completion.Publish == nil {
		return finish(webhook.AttemptResult{Outcome: webhook.AttemptTerminal, ErrorSummary: "publish_config_missing"})
	}
	secrets, err := ParseSecrets(claim.Trigger.SecretConfig)
	if err != nil || secrets.RabbitMQURL == "" {
		return finish(webhook.AttemptResult{Outcome: webhook.AttemptTerminal, ErrorSummary: "rabbitmq_url_missing"})
	}
	body, err := json.Marshal(NewEnvelope(claim))
	if err != nil {
		return finish(webhook.AttemptResult{Outcome: webhook.AttemptTerminal, ErrorSummary: "completion_encoding_failed"})
	}
	timeout := sender.RequestTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	attemptContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	connection, err := amqp091.DialConfig(secrets.RabbitMQURL, amqp091.Config{Dial: amqp091.DefaultDial(timeout)})
	if err != nil {
		return finish(webhook.AttemptResult{Outcome: webhook.AttemptRetry, ErrorSummary: "rabbitmq_connection_failed"})
	}
	defer connection.Close()
	channel, err := connection.Channel()
	if err != nil {
		return finish(webhook.AttemptResult{Outcome: webhook.AttemptRetry, ErrorSummary: "rabbitmq_channel_failed"})
	}
	defer channel.Close()
	if err := channel.Confirm(false); err != nil {
		return finish(webhook.AttemptResult{Outcome: webhook.AttemptRetry, ErrorSummary: "rabbitmq_confirm_failed"})
	}
	returns := channel.NotifyReturn(make(chan amqp091.Return, 1))
	policy := claim.Delivery.Completion.Publish
	confirmation, err := channel.PublishWithDeferredConfirmWithContext(
		attemptContext,
		policy.Exchange,
		policy.RoutingKey,
		true,
		false,
		amqp091.Publishing{
			ContentType:   "application/json",
			DeliveryMode:  amqp091.Persistent,
			MessageId:     claim.Delivery.DeliveryID,
			CorrelationId: claim.Delivery.CorrelationID,
			Type:          EnvelopeType,
			Timestamp:     claim.Run.UpdatedAt.UTC(),
			Body:          body,
			Headers: amqp091.Table{
				"x-windforce-run-id":     claim.Run.ID,
				"x-windforce-trigger-id": claim.Trigger.ID,
			},
		},
	)
	if err != nil {
		return finish(webhook.AttemptResult{Outcome: webhook.AttemptRetry, ErrorSummary: "rabbitmq_publish_failed"})
	}
	acked, err := confirmation.WaitContext(attemptContext)
	if err != nil {
		return finish(webhook.AttemptResult{Outcome: webhook.AttemptRetry, ErrorSummary: "rabbitmq_confirm_timeout"})
	}
	if !acked {
		return finish(webhook.AttemptResult{Outcome: webhook.AttemptRetry, ErrorSummary: "rabbitmq_nack"})
	}
	select {
	case <-returns:
		return finish(webhook.AttemptResult{Outcome: webhook.AttemptTerminal, ErrorSummary: "rabbitmq_unroutable"})
	default:
		return finish(webhook.AttemptResult{Outcome: webhook.AttemptSucceeded})
	}
}
