package deploy

import (
	"context"
	"encoding/json"
	"sync"
)

const liveEventBuffer = 128

type eventSubscription struct {
	ch    chan RunEvent
	after int64
}

type eventBroker struct {
	mu   sync.Mutex
	subs map[int64]map[*eventSubscription]struct{}
}

func newEventBroker() *eventBroker {
	return &eventBroker{subs: make(map[int64]map[*eventSubscription]struct{})}
}

func (s *OrchestrationStore) publish(events []RunEvent) {
	for _, event := range events {
		s.broker.publish(event)
		if event.Type == EventRunState {
			var data struct {
				State RunState `json:"state"`
			}
			if json.Unmarshal(event.Data, &data) == nil && data.State.Terminal() {
				s.broker.closeRun(event.RunID)
			}
		}
	}
}

func (b *eventBroker) publish(event RunEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for sub := range b.subs[event.RunID] {
		if event.Seq <= sub.after {
			continue
		}
		select {
		case sub.ch <- event:
			sub.after = event.Seq
		default:
			delete(b.subs[event.RunID], sub)
			close(sub.ch)
		}
	}
}

func (b *eventBroker) closeRun(runID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for sub := range b.subs[runID] {
		delete(b.subs[runID], sub)
		close(sub.ch)
	}
	delete(b.subs, runID)
}

// Subscribe closes the commit/publish gap by holding the broker lock while it
// reads backlog and registers. A publisher that committed just before the read
// sees sub.after and cannot deliver the same sequence twice.
func (s *OrchestrationStore) Subscribe(
	ctx context.Context,
	runID, after int64,
) ([]RunEvent, <-chan RunEvent, func(), error) {
	s.broker.mu.Lock()
	defer s.broker.mu.Unlock()
	backlog, err := s.EventsAfter(ctx, runID, after, 5000)
	if err != nil {
		return nil, nil, nil, err
	}
	last := after
	for _, event := range backlog {
		if event.Seq > last {
			last = event.Seq
		}
	}
	run, err := s.Run(ctx, runID)
	if err != nil {
		return nil, nil, nil, err
	}
	sub := &eventSubscription{ch: make(chan RunEvent, liveEventBuffer), after: last}
	if !run.State.Terminal() {
		if s.broker.subs[runID] == nil {
			s.broker.subs[runID] = make(map[*eventSubscription]struct{})
		}
		s.broker.subs[runID][sub] = struct{}{}
	} else {
		close(sub.ch)
	}
	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			s.broker.mu.Lock()
			defer s.broker.mu.Unlock()
			if _, ok := s.broker.subs[runID][sub]; ok {
				delete(s.broker.subs[runID], sub)
				close(sub.ch)
			}
		})
	}
	return backlog, sub.ch, unsubscribe, nil
}
