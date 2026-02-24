package daemon

import (
	"context"
	"testing"

	"github.com/uprockcom/maestro/pkg/container"
)

func TestGetContainerContacts_NoLabel(t *testing.T) {
	ops := &mockContainerOps{}
	d := newTestDaemon(ops, nil)

	contacts := d.getContainerContacts("some-container")
	if contacts != nil {
		t.Errorf("expected nil contacts, got %v", contacts)
	}
}

func TestGetContainerContacts_ValidJSON(t *testing.T) {
	ops := &mockContainerOps{
		labels: map[string]map[string]string{
			"maestro-test-1": {
				"maestro.contacts": `{"signal":{"recipient":"+15551234567"}}`,
			},
		},
	}
	d := newTestDaemon(ops, nil)

	contacts := d.getContainerContacts("maestro-test-1")
	if contacts == nil {
		t.Fatal("expected non-nil contacts")
	}
	if sc, ok := contacts["signal"]; !ok {
		t.Error("expected signal key in contacts")
	} else if sc["recipient"] != "+15551234567" {
		t.Errorf("expected recipient '+15551234567', got %q", sc["recipient"])
	}
}

func TestGetContainerContacts_InvalidJSON(t *testing.T) {
	ops := &mockContainerOps{
		labels: map[string]map[string]string{
			"maestro-test-1": {
				"maestro.contacts": `not json`,
			},
		},
	}
	d := newTestDaemon(ops, nil)

	contacts := d.getContainerContacts("maestro-test-1")
	if contacts != nil {
		t.Errorf("expected nil for invalid JSON, got %v", contacts)
	}
}

func TestListContainers_PopulatesContacts(t *testing.T) {
	ops := &mockContainerOps{
		containers: []container.Info{
			{Name: "maestro-feat-auth-1", ShortName: "feat-auth-1"},
			{
				Name: "maestro-feat-wife-1", ShortName: "feat-wife-1",
				Contacts: map[string]map[string]string{
					"signal": {"recipient": "+15551234567"},
				},
			},
		},
	}
	d := newTestDaemon(ops, nil)

	result, err := d.ListContainers(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(result))
	}

	// First container has no contacts
	if result[0].Contacts != nil {
		t.Errorf("expected nil contacts for feat-auth-1, got %v", result[0].Contacts)
	}

	// Second container has contacts
	if result[1].Contacts == nil {
		t.Fatal("expected non-nil contacts for feat-wife-1")
	}
	if sc, ok := result[1].Contacts["signal"]; !ok {
		t.Error("expected signal key")
	} else if sc["recipient"] != "+15551234567" {
		t.Errorf("expected recipient '+15551234567', got %q", sc["recipient"])
	}
}

func TestListContainers_MultipleContactProviders(t *testing.T) {
	ops := &mockContainerOps{
		containers: []container.Info{
			{
				Name: "maestro-multi-1", ShortName: "multi-1",
				Contacts: map[string]map[string]string{
					"signal": {"recipient": "+1555"},
					"slack":  {"channel": "#wife"},
				},
			},
		},
	}
	d := newTestDaemon(ops, nil)

	result, err := d.ListContainers(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].Contacts == nil {
		t.Fatal("expected contacts")
	}
	if len(result[0].Contacts) != 2 {
		t.Errorf("expected 2 provider entries, got %d", len(result[0].Contacts))
	}
}
