package analytics

import (
	"context"
	"sync"
	"time"

	"github.com/posthog/posthog-go"
)

// Event is a sink-ready analytics event: one PostHog capture.
type Event struct {
	DistinctID string
	Event      string
	GroupType  string
	Group      string
	Properties map[string]interface{}
}

// Sink receives events from the recorder's flush loop.
type Sink interface {
	Enqueue(Event)
	Close()
}

// fakeSink captures events in memory for tests.
type fakeSink struct {
	mu     sync.Mutex
	events []Event
}

func (f *fakeSink) Enqueue(e Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
}

func (f *fakeSink) Close() {}

func (f *fakeSink) Snapshot() []Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Event, len(f.events))
	copy(out, f.events)
	return out
}

// noopSink discards events; used when no PostHog API key is configured.
type noopSink struct{}

func (noopSink) Enqueue(Event) {}
func (noopSink) Close()        {}

type posthogSink struct {
	client posthog.Client
}

// NewPostHogSink builds a sink backed by the posthog-go async batched client.
func NewPostHogSink(cfg Config) (Sink, error) {
	// No key → silent no-op. Production guards this earlier (it warns and never
	// calls here with an empty key); this keeps the constructor safe and quiet
	// and avoids the SDK's stderr warning on the empty-key path.
	if cfg.PostHogAPIKey == "" {
		return &noopSink{}, nil
	}
	client, err := posthog.NewWithConfig(cfg.PostHogAPIKey, posthog.Config{
		Endpoint: cfg.PostHogHost,
	})
	if err != nil {
		return nil, err
	}
	return &posthogSink{client: client}, nil
}

func (s *posthogSink) Enqueue(e Event) {
	props := posthog.NewProperties()
	for k, v := range e.Properties {
		props.Set(k, v)
	}
	groups := posthog.NewGroups()
	if e.GroupType != "" {
		groups.Set(e.GroupType, e.Group)
	}
	// Enqueue is non-blocking; the SDK batches and sends asynchronously.
	_ = s.client.Enqueue(posthog.Capture{
		DistinctId: e.DistinctID,
		Event:      e.Event,
		Properties: props,
		Groups:     groups,
	})
}

func (s *posthogSink) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.client.CloseWithContext(ctx)
}
