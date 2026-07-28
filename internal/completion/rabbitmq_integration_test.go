package completion

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/rabbitmq/amqp091-go"

	"github.com/imprun/windforce-core/internal/state"
	"github.com/imprun/windforce-core/internal/webhook"
)

func TestRabbitMQSenderPublishesConfirmedCompletion(t *testing.T) {
	url := os.Getenv("WINDFORCE_RABBITMQ_TEST_URL")
	if url == "" {
		t.Skip("WINDFORCE_RABBITMQ_TEST_URL is not set")
	}
	connection, err := amqp091.Dial(url)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	channel, err := connection.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer channel.Close()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	exchange := "windforce-completion-" + suffix
	routingKey := "completed." + suffix
	if err := channel.ExchangeDeclare(exchange, "direct", false, true, false, false, nil); err != nil {
		t.Fatal(err)
	}
	queue, err := channel.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := channel.QueueBind(queue.Name, routingKey, exchange, false, nil); err != nil {
		t.Fatal(err)
	}
	claim := &state.TriggerCompletionClaim{
		Delivery: state.TriggerDelivery{
			ID:            "trd-rabbitmq-1",
			WorkspaceID:   "workspace-a",
			TriggerID:     "trigger-a",
			DeliveryID:    "source-delivery-1",
			CorrelationID: "correlation-1",
			Completion: state.TriggerCompletionPolicy{
				Mode: state.TriggerCompletionModePublish,
				Publish: &state.TriggerCompletionPublish{
					Exchange:   exchange,
					RoutingKey: routingKey,
				},
			},
		},
		Run: state.Run{
			ID:        "run-rabbitmq-1",
			App:       "orders",
			Action:    "ingest",
			State:     state.RunSucceeded,
			Output:    json.RawMessage(`{"ok":true}`),
			UpdatedAt: time.Now().UTC(),
		},
		Trigger: state.TriggerDefinition{
			ID:           "trigger-a",
			SecretConfig: json.RawMessage(fmt.Sprintf(`{"completion":{"rabbitmq_url":%q}}`, url)),
		},
	}
	result := (RabbitMQSender{RequestTimeout: 10 * time.Second}).Send(context.Background(), claim)
	if result.Outcome != webhook.AttemptSucceeded {
		t.Fatalf("attempt = %#v", result)
	}
	message, ok, err := channel.Get(queue.Name, true)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("completion message was not routed")
	}
	if message.MessageId != claim.Delivery.DeliveryID ||
		message.CorrelationId != claim.Delivery.CorrelationID ||
		message.Type != EnvelopeType {
		t.Fatalf("message = %#v", message)
	}
	var envelope Envelope
	if err := json.Unmarshal(message.Body, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.RunID != claim.Run.ID || envelope.Output == nil {
		t.Fatalf("envelope = %#v", envelope)
	}

	claim.Delivery.Completion.Publish.RoutingKey = "missing." + suffix
	unroutable := (RabbitMQSender{RequestTimeout: 10 * time.Second}).Send(context.Background(), claim)
	if unroutable.Outcome != webhook.AttemptTerminal || unroutable.ErrorSummary != "rabbitmq_unroutable" {
		t.Fatalf("unroutable attempt = %#v", unroutable)
	}
}
