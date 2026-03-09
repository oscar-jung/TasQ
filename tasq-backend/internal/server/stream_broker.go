package server

import (
	"sync"
	"time"
)

type projectSignal struct {
	ProjectID int64     `json:"project_id"`
	TaskID    *int64    `json:"task_id,omitempty"`
	EventType string    `json:"event_type"`
	CreatedAt time.Time `json:"created_at"`
	Payload   any       `json:"payload,omitempty"`
}

type projectStreamBroker struct {
	mu          sync.RWMutex
	subscribers map[int64]map[chan projectSignal]struct{}
}

func newProjectStreamBroker() *projectStreamBroker {
	return &projectStreamBroker{
		subscribers: make(map[int64]map[chan projectSignal]struct{}),
	}
}

func (b *projectStreamBroker) subscribe(projectID int64) chan projectSignal {
	ch := make(chan projectSignal, 16)
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.subscribers[projectID]; !ok {
		b.subscribers[projectID] = make(map[chan projectSignal]struct{})
	}
	b.subscribers[projectID][ch] = struct{}{}
	return ch
}

func (b *projectStreamBroker) unsubscribe(projectID int64, ch chan projectSignal) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if subs, ok := b.subscribers[projectID]; ok {
		delete(subs, ch)
		if len(subs) == 0 {
			delete(b.subscribers, projectID)
		}
	}
	close(ch)
}

func (b *projectStreamBroker) publish(signal projectSignal) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subscribers[signal.ProjectID] {
		select {
		case ch <- signal:
		default:
		}
	}
}
