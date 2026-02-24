package notify

import (
	"context"
	"testing"
	"time"
)

// mockScopedProvider implements ScopedProvider for testing.
type mockScopedProvider struct {
	name      string
	available bool
	isDefault bool
	matchFn   func(Event) bool
	sent      []Event
}

func (m *mockScopedProvider) Name() string                 { return m.name }
func (m *mockScopedProvider) Available() bool               { return m.available }
func (m *mockScopedProvider) Close() error                  { return nil }
func (m *mockScopedProvider) IsDefault() bool               { return m.isDefault }
func (m *mockScopedProvider) MatchesEvent(e Event) bool     { return m.matchFn(e) }

func (m *mockScopedProvider) Send(_ context.Context, e Event) error {
	m.sent = append(m.sent, e)
	return nil
}

func (m *mockScopedProvider) SendInteractive(_ context.Context, e Event) (<-chan Response, bool, error) {
	return nil, false, ErrNotInteractive
}

// mockPlainProvider implements Provider (not ScopedProvider) for testing.
type mockPlainProvider struct {
	name      string
	available bool
	sent      []Event
}

func (m *mockPlainProvider) Name() string                 { return m.name }
func (m *mockPlainProvider) Available() bool               { return m.available }
func (m *mockPlainProvider) Close() error                  { return nil }

func (m *mockPlainProvider) Send(_ context.Context, e Event) error {
	m.sent = append(m.sent, e)
	return nil
}

func (m *mockPlainProvider) SendInteractive(_ context.Context, e Event) (<-chan Response, bool, error) {
	return nil, false, ErrNotInteractive
}

func TestShouldSendTo_PrefixMatching(t *testing.T) {
	providerNotifyOn := map[string][]string{
		"signal": {"question", "blocker"},
	}
	engine := NewEngine(nil, nil, func(string, ...interface{}) {}, providerNotifyOn)

	// signal:4567 should inherit signal's notify_on
	if !engine.shouldSendTo("signal:4567", "question") {
		t.Error("expected true for signal:4567/question (prefix match)")
	}
	if !engine.shouldSendTo("signal:4567", "blocker") {
		t.Error("expected true for signal:4567/blocker (prefix match)")
	}
	if engine.shouldSendTo("signal:4567", "attention_needed") {
		t.Error("expected false for signal:4567/attention_needed (prefix match)")
	}

	// Unknown prefix falls through to default (true)
	if !engine.shouldSendTo("slack:chan", "anything") {
		t.Error("expected true for slack:chan with no override")
	}
}

func TestNotify_ScopedProvider_MatchesEvent(t *testing.T) {
	wifeRecipient := "+15551234567"
	userRecipient := "+15559876543"

	defaultProvider := &mockScopedProvider{
		name:      "signal",
		available: true,
		isDefault: true,
		matchFn: func(e Event) bool {
			if e.Contacts == nil {
				return true
			}
			sc, ok := e.Contacts["signal"]
			if !ok {
				return true
			}
			return sc["recipient"] == userRecipient
		},
	}

	wifeProvider := &mockScopedProvider{
		name:      "signal:4567",
		available: true,
		isDefault: false,
		matchFn: func(e Event) bool {
			if e.Contacts == nil {
				return false
			}
			sc, ok := e.Contacts["signal"]
			if !ok {
				return false
			}
			return sc["recipient"] == wifeRecipient
		},
	}

	engine := NewEngine(
		[]Provider{defaultProvider, wifeProvider},
		nil,
		func(string, ...interface{}) {},
		nil,
	)

	// Event with wife's contacts should only go to wife's provider
	wifeEvent := Event{
		ID:    "test-wife",
		Title: "Question",
		Type:  EventQuestion,
		Contacts: map[string]map[string]string{
			"signal": {"recipient": wifeRecipient},
		},
		Timestamp: time.Now(),
	}
	engine.Notify(wifeEvent)

	if len(defaultProvider.sent) != 0 {
		t.Errorf("default provider should not receive wife's event, got %d", len(defaultProvider.sent))
	}
	if len(wifeProvider.sent) != 1 {
		t.Errorf("wife provider should receive 1 event, got %d", len(wifeProvider.sent))
	}

	// Event with no contacts should only go to default
	defaultEvent := Event{
		ID:        "test-default",
		Title:     "Attention",
		Type:      EventAttentionNeeded,
		Timestamp: time.Now(),
	}
	engine.Notify(defaultEvent)

	if len(defaultProvider.sent) != 1 {
		t.Errorf("default provider should receive 1 event, got %d", len(defaultProvider.sent))
	}
	if len(wifeProvider.sent) != 1 {
		t.Errorf("wife provider should still have 1 event, got %d", len(wifeProvider.sent))
	}
}

func TestNotify_NonScopedProvider_AlwaysMatches(t *testing.T) {
	desktopProvider := &mockPlainProvider{
		name:      "desktop",
		available: true,
	}

	engine := NewEngine(
		[]Provider{desktopProvider},
		nil,
		func(string, ...interface{}) {},
		nil,
	)

	event := Event{
		ID:    "test",
		Title: "Test",
		Type:  EventAttentionNeeded,
		Contacts: map[string]map[string]string{
			"signal": {"recipient": "+155512345"},
		},
		Timestamp: time.Now(),
	}
	engine.Notify(event)

	if len(desktopProvider.sent) != 1 {
		t.Errorf("non-scoped provider should always receive events, got %d", len(desktopProvider.sent))
	}
}

func TestAskQuestion_ScopedProvider_Filtering(t *testing.T) {
	wifeRecipient := "+15551234567"

	// Default provider that only matches events with no contacts
	defaultProvider := &mockScopedProvider{
		name:      "signal",
		available: true,
		isDefault: true,
		matchFn: func(e Event) bool {
			return e.Contacts == nil
		},
	}

	// Wife provider that only matches events with wife's recipient
	wifeProvider := &mockScopedProvider{
		name:      "signal:4567",
		available: true,
		isDefault: false,
		matchFn: func(e Event) bool {
			if e.Contacts == nil {
				return false
			}
			sc, ok := e.Contacts["signal"]
			return ok && sc["recipient"] == wifeRecipient
		},
	}

	callbackCalled := false
	engine := NewEngine(
		[]Provider{defaultProvider, wifeProvider},
		func(resp Response) error {
			callbackCalled = true
			return nil
		},
		func(string, ...interface{}) {},
		nil,
	)

	// AskQuestion with wife's contacts — only wife's provider should receive it
	wifeEvent := Event{
		ID:    "q-wife",
		Title: "Question for wife",
		Type:  EventQuestion,
		Contacts: map[string]map[string]string{
			"signal": {"recipient": wifeRecipient},
		},
		Timestamp: time.Now(),
	}
	engine.AskQuestion(wifeEvent)

	// SendInteractive returns ErrNotInteractive, so falls back to Send
	if len(defaultProvider.sent) != 0 {
		t.Errorf("default provider should not receive wife's question, got %d sends", len(defaultProvider.sent))
	}
	if len(wifeProvider.sent) != 1 {
		t.Errorf("wife provider should receive 1 fallback send, got %d", len(wifeProvider.sent))
	}

	// AskQuestion with no contacts — only default should receive
	defaultEvent := Event{
		ID:        "q-default",
		Title:     "Question for user",
		Type:      EventQuestion,
		Timestamp: time.Now(),
	}
	engine.AskQuestion(defaultEvent)

	if len(defaultProvider.sent) != 1 {
		t.Errorf("default provider should receive 1 fallback send, got %d", len(defaultProvider.sent))
	}
	if len(wifeProvider.sent) != 1 {
		t.Errorf("wife provider should still have 1 send, got %d", len(wifeProvider.sent))
	}

	_ = callbackCalled
}

func TestMatchesScope_NonScoped(t *testing.T) {
	p := &mockPlainProvider{name: "desktop", available: true}
	e := Event{
		Contacts: map[string]map[string]string{
			"signal": {"recipient": "+1555"},
		},
	}
	if !matchesScope(p, e) {
		t.Error("non-scoped provider should always match")
	}
}

func TestMatchesScope_Scoped(t *testing.T) {
	p := &mockScopedProvider{
		name:      "signal",
		available: true,
		isDefault: true,
		matchFn:   func(e Event) bool { return e.Contacts == nil },
	}

	if !matchesScope(p, Event{}) {
		t.Error("should match event with nil contacts")
	}

	if matchesScope(p, Event{Contacts: map[string]map[string]string{"signal": {"recipient": "+1"}}}) {
		t.Error("should not match event with contacts")
	}
}
