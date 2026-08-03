package state

import (
	"context"
	"strings"
	"sync"
	"time"
)

// HumanTaskChangeSubscriber is an optional state-store capability used by the
// HTTP runtime waiter. The channel is only a wake-up hint; callers must always
// re-read the durable HumanTask after it fires.
type HumanTaskChangeSubscriber interface {
	SubscribeHumanTaskChanges(taskID string) (<-chan struct{}, func())
}

// HumanTaskDeadlineStore is the state-store capability used by the independent
// deadline sweeper. Implementations must atomically expire only pending hold
// tasks whose persisted deadline is at or before now.
type HumanTaskDeadlineStore interface {
	ExpireDueHeldHumanTasks(ctx context.Context, now time.Time, limit int) (int64, error)
}

type humanTaskSignalHub struct {
	mu          sync.Mutex
	nextID      uint64
	subscribers map[string]map[uint64]chan struct{}
}

func (h *humanTaskSignalHub) subscribe(taskID string) (<-chan struct{}, func()) {
	taskID = strings.TrimSpace(taskID)
	channel := make(chan struct{}, 1)
	h.mu.Lock()
	if h.subscribers == nil {
		h.subscribers = make(map[string]map[uint64]chan struct{})
	}
	h.nextID++
	subscriptionID := h.nextID
	if h.subscribers[taskID] == nil {
		h.subscribers[taskID] = make(map[uint64]chan struct{})
	}
	h.subscribers[taskID][subscriptionID] = channel
	h.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			if subscriptions := h.subscribers[taskID]; subscriptions != nil {
				delete(subscriptions, subscriptionID)
				if len(subscriptions) == 0 {
					delete(h.subscribers, taskID)
				}
			}
			h.mu.Unlock()
		})
	}
	return channel, cancel
}

func (h *humanTaskSignalHub) notify(taskIDs ...string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, taskID := range taskIDs {
		for _, channel := range h.subscribers[strings.TrimSpace(taskID)] {
			select {
			case channel <- struct{}{}:
			default:
			}
		}
	}
}
