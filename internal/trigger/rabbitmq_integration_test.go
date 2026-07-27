package trigger

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/execution"
	"github.com/imprun/windforce-core/internal/state"
	amqp "github.com/rabbitmq/amqp091-go"
)

type sequenceAdmission struct {
	mu       sync.Mutex
	failures []error
	calls    int
}

func (a *sequenceAdmission) CreateRun(context.Context, execution.CreateRunRequest) (execution.Admission, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	if len(a.failures) > 0 {
		err := a.failures[0]
		a.failures = a.failures[1:]
		return execution.Admission{}, err
	}
	return execution.Admission{Run: state.Run{ID: fmt.Sprintf("run-%d", a.calls)}}, nil
}

func (a *sequenceAdmission) callCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

func TestRabbitMQIntegrationAdmissionAndDeadLetter(t *testing.T) {
	url := os.Getenv("WINDFORCE_RABBITMQ_TEST_URL")
	if url == "" {
		t.Skip("WINDFORCE_RABBITMQ_TEST_URL is not set")
	}
	connection, err := amqp.Dial(url)
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
	sourceQueue := "wf-trigger-source-" + suffix
	deadQueue := "wf-trigger-dead-" + suffix
	if _, err := channel.QueueDeclare(deadQueue, false, false, false, false, nil); err != nil {
		t.Fatal(err)
	}
	defer channel.QueueDelete(deadQueue, false, false, false)
	if _, err := channel.QueueDeclare(sourceQueue, false, false, false, false, amqp.Table{
		"x-dead-letter-exchange":    "",
		"x-dead-letter-routing-key": deadQueue,
	}); err != nil {
		t.Fatal(err)
	}
	defer channel.QueueDelete(sourceQueue, false, false, false)

	t.Run("retryable delivery is requeued and then acknowledged", func(t *testing.T) {
		admission := &sequenceAdmission{failures: []error{
			&execution.Fault{Kind: execution.FaultUnavailable, Message: "temporary"},
		}}
		adapter := newRabbitMQIntegrationTrigger(t, url, sourceQueue, "retry", admission)
		if err := adapter.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		publishRabbitMQIntegrationMessage(t, channel, sourceQueue, "message-retry")
		waitRabbitMQCondition(t, 10*time.Second, func() bool {
			return admission.callCount() >= 2
		})
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := adapter.Stop(stopCtx); err != nil {
			t.Fatal(err)
		}
		queue, err := channel.QueueInspect(sourceQueue)
		if err != nil {
			t.Fatal(err)
		}
		if queue.Messages != 0 {
			t.Fatalf("source queue still has %d message(s)", queue.Messages)
		}
	})

	t.Run("terminal delivery is rejected to the dead-letter queue", func(t *testing.T) {
		admission := &sequenceAdmission{failures: []error{
			&execution.Fault{Kind: execution.FaultInvalidRequest, Message: "terminal"},
		}}
		adapter := newRabbitMQIntegrationTrigger(t, url, sourceQueue, "terminal", admission)
		if err := adapter.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		publishRabbitMQIntegrationMessage(t, channel, sourceQueue, "message-terminal")
		waitRabbitMQCondition(t, 10*time.Second, func() bool {
			message, ok, err := channel.Get(deadQueue, true)
			if err != nil {
				t.Fatal(err)
			}
			return ok && message.MessageId == "message-terminal"
		})
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := adapter.Stop(stopCtx); err != nil {
			t.Fatal(err)
		}
	})
}

func newRabbitMQIntegrationTrigger(t *testing.T, url string, queue string, id string, admission AdmissionService) *RabbitMQTrigger {
	t.Helper()
	definition := state.TriggerDefinition{
		ID:           "trg-" + id,
		WorkspaceID:  "ws",
		Name:         id,
		Kind:         KindRabbitMQ,
		AppKey:       "demo",
		ActionKey:    "run",
		Config:       json.RawMessage(fmt.Sprintf(`{"queue":%q,"prefetch":1,"concurrency":1}`, queue)),
		SecretConfig: json.RawMessage(fmt.Sprintf(`{"url":%q}`, url)),
	}
	value, err := NewRabbitMQTrigger(definition)
	if err != nil {
		t.Fatal(err)
	}
	adapter := value.(*RabbitMQTrigger)
	if err := adapter.Initialize(Runtime{Submitter: &Submitter{
		Store:     &triggerTestStore{},
		Admission: admission,
	}}); err != nil {
		t.Fatal(err)
	}
	return adapter
}

func publishRabbitMQIntegrationMessage(t *testing.T, channel *amqp.Channel, queue string, messageID string) {
	t.Helper()
	if err := channel.PublishWithContext(context.Background(), "", queue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		MessageId:    messageID,
		Body:         []byte(`{"ok":true}`),
	}); err != nil {
		t.Fatal(err)
	}
}

func waitRabbitMQCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timed out waiting for RabbitMQ condition")
}
