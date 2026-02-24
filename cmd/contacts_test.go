package cmd

import (
	"encoding/json"
	"testing"
)

func TestResolveContacts_BothFlagsError(t *testing.T) {
	_, err := resolveContacts(`{"signal":{}}`, "wife")
	if err == nil {
		t.Fatal("expected error when both flags provided")
	}
}

func TestResolveContacts_NoFlags(t *testing.T) {
	result, err := resolveContacts("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestResolveContacts_RawJSON(t *testing.T) {
	input := `{"signal":{"recipient":"+15551234567"}}`
	result, err := resolveContacts(input, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be valid JSON
	var parsed map[string]map[string]string
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if parsed["signal"]["recipient"] != "+15551234567" {
		t.Errorf("expected recipient '+15551234567', got %q", parsed["signal"]["recipient"])
	}
}

func TestResolveContacts_InvalidJSON(t *testing.T) {
	_, err := resolveContacts("not json", "")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestResolveContacts_ProfileNotFound(t *testing.T) {
	// Save and restore global config
	origConfig := config
	defer func() { config = origConfig }()

	config = &Config{
		Contacts: map[string]ContactProfile{},
	}

	_, err := resolveContacts("", "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
}

func TestResolveContacts_ProfileNilContacts(t *testing.T) {
	origConfig := config
	defer func() { config = origConfig }()

	config = &Config{}

	_, err := resolveContacts("", "wife")
	if err == nil {
		t.Fatal("expected error when no contacts defined")
	}
}

func TestResolveContacts_ProfileWithSignal(t *testing.T) {
	origConfig := config
	defer func() { config = origConfig }()

	config = &Config{
		Contacts: map[string]ContactProfile{
			"wife": {
				Signal: &SignalContactOverride{
					Recipient: "+15559876543",
					APIKey:    "key123",
				},
			},
		},
	}

	result, err := resolveContacts("", "wife")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed map[string]map[string]string
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if parsed["signal"]["recipient"] != "+15559876543" {
		t.Errorf("expected recipient '+15559876543', got %q", parsed["signal"]["recipient"])
	}
}

func TestResolveContacts_ProfileNoSignal(t *testing.T) {
	origConfig := config
	defer func() { config = origConfig }()

	config = &Config{
		Contacts: map[string]ContactProfile{
			"wife": {
				Signal: nil, // No signal override
			},
		},
	}

	_, err := resolveContacts("", "wife")
	if err == nil {
		t.Fatal("expected error for profile with no contact methods")
	}
}
