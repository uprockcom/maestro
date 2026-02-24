package signal

import (
	"testing"

	"github.com/uprockcom/maestro/pkg/notify"
)

func TestNew_DefaultProvider(t *testing.T) {
	sp := New(Config{
		Number:    "+1bot",
		Recipient: "+1user",
		URL:       "http://localhost:8080",
		APIKey:    "key123",
	}, nil, func(string, ...interface{}) {}, true)

	if sp.Name() != "signal" {
		t.Errorf("expected name 'signal', got %q", sp.Name())
	}
	if !sp.IsDefault() {
		t.Error("expected IsDefault=true")
	}
	if sp.recipient != "+1user" {
		t.Errorf("expected recipient '+1user', got %q", sp.recipient)
	}
}

func TestNew_OverrideProvider_Name(t *testing.T) {
	sp := New(Config{
		Number:    "+1bot",
		Recipient: "+15551234567",
		URL:       "http://localhost:8080",
		APIKey:    "key456",
	}, nil, func(string, ...interface{}) {}, false)

	if sp.Name() != "signal:4567" {
		t.Errorf("expected name 'signal:4567', got %q", sp.Name())
	}
	if sp.IsDefault() {
		t.Error("expected IsDefault=false")
	}
}

func TestNew_ShortRecipient_Name(t *testing.T) {
	sp := New(Config{
		Number:    "+1bot",
		Recipient: "+12",
		URL:       "http://localhost:8080",
		APIKey:    "key",
	}, nil, func(string, ...interface{}) {}, false)

	// Recipient shorter than 4 chars, use full string
	if sp.Name() != "signal:+12" {
		t.Errorf("expected name 'signal:+12', got %q", sp.Name())
	}
}

func TestMatchesEvent_DefaultProvider(t *testing.T) {
	sp := New(Config{
		Number:    "+1bot",
		Recipient: "+1user",
		URL:       "http://localhost:8080",
		APIKey:    "key",
	}, nil, func(string, ...interface{}) {}, true)

	tests := []struct {
		name     string
		event    notify.Event
		expected bool
	}{
		{
			name:     "nil contacts → default matches",
			event:    notify.Event{},
			expected: true,
		},
		{
			name: "no signal key → default matches",
			event: notify.Event{
				Contacts: map[string]map[string]string{
					"slack": {"channel": "#general"},
				},
			},
			expected: true,
		},
		{
			name: "signal matches own recipient → true",
			event: notify.Event{
				Contacts: map[string]map[string]string{
					"signal": {"recipient": "+1user"},
				},
			},
			expected: true,
		},
		{
			name: "signal matches different recipient → false",
			event: notify.Event{
				Contacts: map[string]map[string]string{
					"signal": {"recipient": "+1wife"},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sp.MatchesEvent(tt.event); got != tt.expected {
				t.Errorf("MatchesEvent() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestMatchesEvent_OverrideProvider(t *testing.T) {
	sp := New(Config{
		Number:    "+1bot",
		Recipient: "+1wife",
		URL:       "http://localhost:8080",
		APIKey:    "key",
	}, nil, func(string, ...interface{}) {}, false)

	tests := []struct {
		name     string
		event    notify.Event
		expected bool
	}{
		{
			name:     "nil contacts → non-default does not match",
			event:    notify.Event{},
			expected: false,
		},
		{
			name: "no signal key → non-default does not match",
			event: notify.Event{
				Contacts: map[string]map[string]string{
					"slack": {"channel": "#general"},
				},
			},
			expected: false,
		},
		{
			name: "signal matches own recipient → true",
			event: notify.Event{
				Contacts: map[string]map[string]string{
					"signal": {"recipient": "+1wife"},
				},
			},
			expected: true,
		},
		{
			name: "signal matches different recipient → false",
			event: notify.Event{
				Contacts: map[string]map[string]string{
					"signal": {"recipient": "+1user"},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sp.MatchesEvent(tt.event); got != tt.expected {
				t.Errorf("MatchesEvent() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestFilterByContacts_DefaultProvider(t *testing.T) {
	sp := New(Config{
		Number:    "+1bot",
		Recipient: "+1user",
		URL:       "http://localhost:8080",
		APIKey:    "key",
	}, nil, func(string, ...interface{}) {}, true)

	containers := []notify.ContainerSummary{
		{Name: "no-contacts", ShortName: "no-contacts"},
		{
			Name:      "user-container",
			ShortName: "user-container",
			Contacts: map[string]map[string]string{
				"signal": {"recipient": "+1user"},
			},
		},
		{
			Name:      "wife-container",
			ShortName: "wife-container",
			Contacts: map[string]map[string]string{
				"signal": {"recipient": "+1wife"},
			},
		},
		{
			Name:      "slack-only",
			ShortName: "slack-only",
			Contacts: map[string]map[string]string{
				"slack": {"channel": "#general"},
			},
		},
	}

	result := sp.filterByContacts(containers)

	// Default should see: no-contacts, user-container, slack-only (not wife-container)
	if len(result) != 3 {
		t.Fatalf("expected 3 visible containers, got %d", len(result))
	}

	names := map[string]bool{}
	for _, c := range result {
		names[c.Name] = true
	}
	if names["wife-container"] {
		t.Error("wife-container should not be visible to default provider")
	}
	if !names["no-contacts"] || !names["user-container"] || !names["slack-only"] {
		t.Errorf("missing expected containers: %v", names)
	}
}

func TestFilterByContacts_OverrideProvider(t *testing.T) {
	sp := New(Config{
		Number:    "+1bot",
		Recipient: "+1wife",
		URL:       "http://localhost:8080",
		APIKey:    "key",
	}, nil, func(string, ...interface{}) {}, false)

	containers := []notify.ContainerSummary{
		{Name: "no-contacts", ShortName: "no-contacts"},
		{
			Name:      "user-container",
			ShortName: "user-container",
			Contacts: map[string]map[string]string{
				"signal": {"recipient": "+1user"},
			},
		},
		{
			Name:      "wife-container",
			ShortName: "wife-container",
			Contacts: map[string]map[string]string{
				"signal": {"recipient": "+1wife"},
			},
		},
	}

	result := sp.filterByContacts(containers)

	// Wife provider should see only wife-container
	if len(result) != 1 {
		t.Fatalf("expected 1 visible container, got %d", len(result))
	}
	if result[0].Name != "wife-container" {
		t.Errorf("expected wife-container, got %q", result[0].Name)
	}
}

func TestFilterByContacts_EmptyContacts(t *testing.T) {
	sp := New(Config{
		Number:    "+1bot",
		Recipient: "+1wife",
		URL:       "http://localhost:8080",
		APIKey:    "key",
	}, nil, func(string, ...interface{}) {}, false)

	containers := []notify.ContainerSummary{
		{Name: "c1", ShortName: "c1", Contacts: map[string]map[string]string{}},
	}

	result := sp.filterByContacts(containers)

	// Empty contacts map — non-default provider should not see it
	if len(result) != 0 {
		t.Errorf("expected 0 visible, got %d", len(result))
	}
}
