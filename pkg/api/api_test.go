package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/uprockcom/maestro/pkg/api"
)

func TestEndpointParsing(t *testing.T) {
	ep := api.NewEndpoint[struct{}, struct{}]("GET /api/v1/test")
	if ep.Method != "GET" {
		t.Errorf("expected GET, got %s", ep.Method)
	}
	if ep.Path != "/api/v1/test" {
		t.Errorf("expected /api/v1/test, got %s", ep.Path)
	}
	if ep.Pattern != "GET /api/v1/test" {
		t.Errorf("expected full pattern, got %s", ep.Pattern)
	}
}

func TestEndpointParsing_POST(t *testing.T) {
	ep := api.NewEndpoint[struct{}, struct{}]("POST /api/v1/action")
	if ep.Method != "POST" {
		t.Errorf("expected POST, got %s", ep.Method)
	}
	if ep.Path != "/api/v1/action" {
		t.Errorf("expected /api/v1/action, got %s", ep.Path)
	}
}

func TestEndpointParsing_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for invalid pattern")
		}
	}()
	api.NewEndpoint[struct{}, struct{}]("invalid")
}

func TestRoundTrip_GET(t *testing.T) {
	type Resp struct {
		Message string `json:"message"`
	}
	ep := api.NewEndpoint[struct{}, Resp]("GET /api/v1/hello")

	mux := http.NewServeMux()
	api.Handle(mux, ep, func(r *http.Request, _ struct{}) (Resp, error) {
		return Resp{Message: "hello"}, nil
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := &api.Client{
		BaseURL:    srv.URL,
		Token:      "test-token",
		HTTPClient: srv.Client(),
	}

	resp, err := api.Call(context.Background(), client, ep, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message != "hello" {
		t.Errorf("expected 'hello', got %q", resp.Message)
	}
}

func TestRoundTrip_POST(t *testing.T) {
	type Req struct {
		Name string `json:"name"`
	}
	type Resp struct {
		Greeting string `json:"greeting"`
	}
	ep := api.NewEndpoint[Req, Resp]("POST /api/v1/greet")

	mux := http.NewServeMux()
	api.Handle(mux, ep, func(r *http.Request, req Req) (Resp, error) {
		return Resp{Greeting: "hello " + req.Name}, nil
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := &api.Client{
		BaseURL:    srv.URL,
		Token:      "test-token",
		HTTPClient: srv.Client(),
	}

	resp, err := api.Call(context.Background(), client, ep, &Req{Name: "world"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Greeting != "hello world" {
		t.Errorf("expected 'hello world', got %q", resp.Greeting)
	}
}

func TestRoundTrip_Error(t *testing.T) {
	type Resp struct{}
	ep := api.NewEndpoint[struct{}, Resp]("GET /api/v1/fail")

	mux := http.NewServeMux()
	api.Handle(mux, ep, func(r *http.Request, _ struct{}) (Resp, error) {
		return Resp{}, &api.Error{Status: 409, Message: "conflict"}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := &api.Client{
		BaseURL:    srv.URL,
		Token:      "test-token",
		HTTPClient: srv.Client(),
	}

	_, err := api.Call(context.Background(), client, ep, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*api.Error)
	if !ok {
		t.Fatalf("expected *api.Error, got %T", err)
	}
	if apiErr.Status != 409 {
		t.Errorf("expected status 409, got %d", apiErr.Status)
	}
	if apiErr.Message != "conflict" {
		t.Errorf("expected 'conflict', got %q", apiErr.Message)
	}
}

func TestRoundTrip_InternalError(t *testing.T) {
	type Resp struct{}
	ep := api.NewEndpoint[struct{}, Resp]("GET /api/v1/internal-fail")

	mux := http.NewServeMux()
	api.Handle(mux, ep, func(r *http.Request, _ struct{}) (Resp, error) {
		return Resp{}, fmt.Errorf("something went wrong")
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := &api.Client{
		BaseURL:    srv.URL,
		Token:      "test-token",
		HTTPClient: srv.Client(),
	}

	_, err := api.Call(context.Background(), client, ep, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*api.Error)
	if !ok {
		t.Fatalf("expected *api.Error, got %T", err)
	}
	if apiErr.Status != 500 {
		t.Errorf("expected status 500, got %d", apiErr.Status)
	}
}

func TestContainerInfo_JSON(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	info := api.ContainerInfo{
		Name:       "maestro-test-1",
		ShortName:  "test-1",
		Status:     "running",
		Branch:     "main",
		AgentState: "active",
		IsDormant:  false,
		CreatedAt:  now,
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded api.ContainerInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Name != info.Name {
		t.Errorf("Name: expected %q, got %q", info.Name, decoded.Name)
	}
	if decoded.ShortName != info.ShortName {
		t.Errorf("ShortName: expected %q, got %q", info.ShortName, decoded.ShortName)
	}
	if decoded.Status != info.Status {
		t.Errorf("Status: expected %q, got %q", info.Status, decoded.Status)
	}
	if decoded.IsDormant != info.IsDormant {
		t.Errorf("IsDormant: expected %v, got %v", info.IsDormant, decoded.IsDormant)
	}
	if !decoded.CreatedAt.Equal(info.CreatedAt) {
		t.Errorf("CreatedAt: expected %v, got %v", info.CreatedAt, decoded.CreatedAt)
	}
}

func TestListContainersRoundTrip(t *testing.T) {
	mux := http.NewServeMux()
	api.Handle(mux, api.ListContainers, func(r *http.Request, req api.ListContainersRequest) (api.ListContainersResponse, error) {
		containers := []api.ContainerInfo{
			{Name: "maestro-test-1", ShortName: "test-1", Status: "running"},
			{Name: "maestro-test-2", ShortName: "test-2", Status: "exited"},
		}
		if req.RunningOnly {
			containers = containers[:1]
		}
		return api.ListContainersResponse{
			Containers: containers,
			StateHash:  "abc123",
			CachedAt:   time.Now(),
			FromCache:  true,
		}, nil
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := &api.Client{
		BaseURL:    srv.URL,
		Token:      "test",
		HTTPClient: srv.Client(),
	}

	// All containers
	resp, err := api.Call(context.Background(), client, api.ListContainers, &api.ListContainersRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Containers) != 2 {
		t.Errorf("expected 2 containers, got %d", len(resp.Containers))
	}
	if resp.StateHash != "abc123" {
		t.Errorf("expected state hash 'abc123', got %q", resp.StateHash)
	}

	// Running only
	resp, err = api.Call(context.Background(), client, api.ListContainers, &api.ListContainersRequest{RunningOnly: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Containers) != 1 {
		t.Errorf("expected 1 container, got %d", len(resp.Containers))
	}
}

func TestStopContainerRoundTrip(t *testing.T) {
	mux := http.NewServeMux()
	api.Handle(mux, api.StopContainer, func(r *http.Request, req api.StopContainerRequest) (api.StopContainerResponse, error) {
		if req.StateHash != "valid-hash" {
			return api.StopContainerResponse{}, api.ErrStateHashMismatch
		}
		return api.StopContainerResponse{
			Success: true,
			Message: "stopped " + req.Name,
		}, nil
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := &api.Client{
		BaseURL:    srv.URL,
		Token:      "test",
		HTTPClient: srv.Client(),
	}

	// Valid hash
	resp, err := api.Call(context.Background(), client, api.StopContainer, &api.StopContainerRequest{
		Name:      "maestro-test-1",
		StateHash: "valid-hash",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Success {
		t.Error("expected success")
	}

	// Invalid hash — should get 409
	_, err = api.Call(context.Background(), client, api.StopContainer, &api.StopContainerRequest{
		Name:      "maestro-test-1",
		StateHash: "stale-hash",
	})
	if err == nil {
		t.Fatal("expected error for stale hash")
	}
	apiErr, ok := err.(*api.Error)
	if !ok {
		t.Fatalf("expected *api.Error, got %T", err)
	}
	if apiErr.Status != 409 {
		t.Errorf("expected 409, got %d", apiErr.Status)
	}
}

func TestErrorString(t *testing.T) {
	err := &api.Error{Status: 404, Message: "not found"}
	expected := "api error 404: not found"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestNewClientFromConfig_NoFile(t *testing.T) {
	client, err := api.NewClientFromConfig("/nonexistent/path")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if client != nil {
		t.Error("expected nil client when file doesn't exist")
	}
}

func TestNewClientFromConfig_ValidFile(t *testing.T) {
	dir := t.TempDir()
	info := api.DaemonIPCInfo{
		Port:  12345,
		Token: "test-token",
		PID:   999,
	}
	data, _ := json.Marshal(info)
	if err := os.WriteFile(filepath.Join(dir, "daemon-ipc.json"), data, 0600); err != nil {
		t.Fatal(err)
	}

	client, err := api.NewClientFromConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.Token != "test-token" {
		t.Errorf("expected token 'test-token', got %q", client.Token)
	}
	if client.BaseURL != "http://127.0.0.1:12345" {
		t.Errorf("expected base URL with port 12345, got %q", client.BaseURL)
	}
}

func TestNewClientFromConfig_ZeroPort(t *testing.T) {
	dir := t.TempDir()
	info := api.DaemonIPCInfo{Port: 0, Token: "tok"}
	data, _ := json.Marshal(info)
	os.WriteFile(filepath.Join(dir, "daemon-ipc.json"), data, 0600)

	client, err := api.NewClientFromConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client != nil {
		t.Error("expected nil client for port 0")
	}
}

// Integration test: server-side auth middleware rejects bad tokens
func TestAuth_ServerRejectsNoToken(t *testing.T) {
	mux := http.NewServeMux()
	api.Handle(mux, api.GetStatus, func(r *http.Request, _ struct{}) (api.StatusResponse, error) {
		// Simulate auth check like withAuth does in daemon
		token := r.Header.Get("X-Maestro-Token")
		if token != "valid-token" {
			return api.StatusResponse{}, &api.Error{Status: 401, Message: "unauthorized"}
		}
		return api.StatusResponse{Running: true, PID: 123}, nil
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Client with no token
	client := &api.Client{
		BaseURL:    srv.URL,
		Token:      "",
		HTTPClient: srv.Client(),
	}

	_, err := api.Call(context.Background(), client, api.GetStatus, nil)
	if err == nil {
		t.Fatal("expected auth rejection")
	}
	apiErr, ok := err.(*api.Error)
	if !ok {
		t.Fatalf("expected *api.Error, got %T: %v", err, err)
	}
	if apiErr.Status != 401 {
		t.Errorf("expected 401, got %d", apiErr.Status)
	}

	// Client with wrong token
	client.Token = "wrong-token"
	_, err = api.Call(context.Background(), client, api.GetStatus, nil)
	if err == nil {
		t.Fatal("expected auth rejection for wrong token")
	}

	// Client with correct token
	client.Token = "valid-token"
	resp, err := api.Call(context.Background(), client, api.GetStatus, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Running {
		t.Error("expected Running=true")
	}
}

// Integration test: cleanup with 409 conflict
func TestCleanup_409Conflict(t *testing.T) {
	mux := http.NewServeMux()
	api.Handle(mux, api.CleanupContainers, func(r *http.Request, req api.CleanupContainersRequest) (api.CleanupContainersResponse, error) {
		if req.StateHash != "current-hash" {
			return api.CleanupContainersResponse{}, api.ErrStateHashMismatch
		}
		return api.CleanupContainersResponse{
			Removed:        req.Names,
			VolumesRemoved: len(req.Names) * 4,
		}, nil
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := &api.Client{
		BaseURL:    srv.URL,
		Token:      "test",
		HTTPClient: srv.Client(),
	}

	// Stale hash should get 409
	_, err := api.Call(context.Background(), client, api.CleanupContainers, &api.CleanupContainersRequest{
		Names:     []string{"a", "b"},
		StateHash: "stale-hash",
	})
	if err == nil {
		t.Fatal("expected 409 error")
	}
	apiErr, ok := err.(*api.Error)
	if !ok {
		t.Fatalf("expected *api.Error, got %T", err)
	}
	if apiErr.Status != 409 {
		t.Errorf("expected 409, got %d", apiErr.Status)
	}

	// Current hash should succeed
	resp, err := api.Call(context.Background(), client, api.CleanupContainers, &api.CleanupContainersRequest{
		Names:     []string{"a", "b"},
		StateHash: "current-hash",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Removed) != 2 {
		t.Errorf("expected 2 removed, got %d", len(resp.Removed))
	}
	if resp.VolumesRemoved != 8 {
		t.Errorf("expected 8 volumes, got %d", resp.VolumesRemoved)
	}
}

// Integration test: client handles unreachable daemon gracefully
func TestClient_UnreachableDaemon(t *testing.T) {
	// Create a client pointing to a port that's not listening
	client := &api.Client{
		BaseURL: "http://127.0.0.1:1", // port 1, very unlikely to be open
		Token:   "test",
		HTTPClient: &http.Client{
			Timeout: 100 * time.Millisecond,
		},
	}

	_, err := api.Call(context.Background(), client, api.GetStatus, nil)
	if err == nil {
		t.Fatal("expected connection error for unreachable daemon")
	}
	// Should NOT be an *api.Error (it's a network error)
	if _, ok := err.(*api.Error); ok {
		t.Error("network errors should not be *api.Error")
	}
}

// Integration test: notification endpoint round-trip
func TestNotification_RoundTrip(t *testing.T) {
	mux := http.NewServeMux()
	api.Handle(mux, api.GetPendingNotifications, func(r *http.Request, _ struct{}) (api.ListPendingNotificationsResponse, error) {
		return api.ListPendingNotificationsResponse{
			Questions: []api.PendingQuestion{
				{
					Event: api.Event{
						ID:            "evt-1",
						ContainerName: "maestro-test-1",
						ShortName:     "test-1",
						Title:         "Test Question",
						Message:       "Pick one",
						Type:          api.EventType("question"),
						Question: &api.QuestionData{
							Questions: []api.QuestionItem{
								{
									Question: "Which approach?",
									Header:   "Approach",
									Options: []api.QuestionOption{
										{Label: "A", Description: "First option"},
										{Label: "B", Description: "Second option"},
									},
									MultiSelect: false,
								},
							},
						},
					},
					SentAt: time.Now(),
				},
			},
		}, nil
	})

	api.Handle(mux, api.AnswerNotification, func(r *http.Request, req api.AnswerNotificationRequest) (api.AnswerNotificationResponse, error) {
		if req.EventID != "evt-1" {
			return api.AnswerNotificationResponse{}, &api.Error{Status: 400, Message: "unknown event"}
		}
		if len(req.Selections) != 1 || req.Selections[0] != "A" {
			return api.AnswerNotificationResponse{}, &api.Error{Status: 400, Message: "unexpected selection"}
		}
		return api.AnswerNotificationResponse{Success: true}, nil
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := &api.Client{BaseURL: srv.URL, Token: "test", HTTPClient: srv.Client()}

	// Get pending
	pending, err := api.Call(context.Background(), client, api.GetPendingNotifications, nil)
	if err != nil {
		t.Fatalf("GetPendingNotifications failed: %v", err)
	}
	if len(pending.Questions) != 1 {
		t.Fatalf("expected 1 question, got %d", len(pending.Questions))
	}
	q := pending.Questions[0]
	if q.Event.ID != "evt-1" {
		t.Errorf("expected event ID evt-1, got %s", q.Event.ID)
	}
	if len(q.Event.Question.Questions) != 1 {
		t.Fatalf("expected 1 question item, got %d", len(q.Event.Question.Questions))
	}
	if len(q.Event.Question.Questions[0].Options) != 2 {
		t.Errorf("expected 2 options, got %d", len(q.Event.Question.Questions[0].Options))
	}

	// Answer
	ansResp, err := api.Call(context.Background(), client, api.AnswerNotification, &api.AnswerNotificationRequest{
		EventID:    "evt-1",
		Selections: []string{"A"},
	})
	if err != nil {
		t.Fatalf("AnswerNotification failed: %v", err)
	}
	if !ansResp.Success {
		t.Error("expected success=true")
	}
}

// Integration test: NewClientFromConfig with invalid JSON
func TestNewClientFromConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "daemon-ipc.json"), []byte("{invalid"), 0600)

	_, err := api.NewClientFromConfig(dir)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// Integration test: GET with multiple query params
func TestRoundTrip_GET_WithQueryParams(t *testing.T) {
	type Req struct {
		Filter string `json:"filter,omitempty"`
		Limit  int    `json:"limit,omitempty"`
		Active bool   `json:"active,omitempty"`
	}
	type Resp struct {
		Filter string `json:"filter"`
		Limit  int    `json:"limit"`
		Active bool   `json:"active"`
	}
	ep := api.NewEndpoint[Req, Resp]("GET /api/v1/search")

	mux := http.NewServeMux()
	api.Handle(mux, ep, func(r *http.Request, req Req) (Resp, error) {
		return Resp{Filter: req.Filter, Limit: req.Limit, Active: req.Active}, nil
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := &api.Client{BaseURL: srv.URL, Token: "t", HTTPClient: srv.Client()}

	resp, err := api.Call(context.Background(), client, ep, &Req{
		Filter: "test",
		Limit:  10,
		Active: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Filter != "test" {
		t.Errorf("expected filter 'test', got %q", resp.Filter)
	}
	if resp.Active != true {
		t.Error("expected active=true")
	}
}
