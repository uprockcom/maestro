package containerservice

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/uprockcom/maestro/pkg/api"
	"github.com/uprockcom/maestro/pkg/container"
)

// newTestDaemonService creates a daemonService backed by a test HTTP server.
func newTestDaemonService(t *testing.T, mux *http.ServeMux) (*daemonService, *httptest.Server) {
	t.Helper()
	ts := httptest.NewServer(mux)
	client := &api.Client{
		BaseURL:    ts.URL,
		Token:      "test-token",
		HTTPClient: ts.Client(),
	}
	return &daemonService{client: client, prefix: "maestro-"}, ts
}

func TestDaemonService_ListAll(t *testing.T) {
	mux := http.NewServeMux()
	api.Handle(mux, api.ListContainers, func(r *http.Request, req api.ListContainersRequest) (api.ListContainersResponse, error) {
		if req.RunningOnly {
			t.Error("ListAll should not set RunningOnly=true")
		}
		return api.ListContainersResponse{
			Containers: []api.ContainerInfo{
				{Name: "maestro-test-1", ShortName: "test-1", Status: "running", Branch: "main"},
				{Name: "maestro-test-2", ShortName: "test-2", Status: "exited", Branch: "dev"},
			},
			StateHash: "hash123",
			CachedAt:  time.Now(),
			FromCache: true,
		}, nil
	})

	svc, ts := newTestDaemonService(t, mux)
	defer ts.Close()

	containers, err := svc.ListAll(context.Background())
	if err != nil {
		t.Fatalf("ListAll failed: %v", err)
	}
	if len(containers) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(containers))
	}
	if containers[0].Name != "maestro-test-1" {
		t.Errorf("expected name maestro-test-1, got %s", containers[0].Name)
	}
	if containers[1].Status != "exited" {
		t.Errorf("expected status exited, got %s", containers[1].Status)
	}
	if svc.StateHash() != "hash123" {
		t.Errorf("expected state hash hash123, got %s", svc.StateHash())
	}
}

func TestDaemonService_ListRunning(t *testing.T) {
	mux := http.NewServeMux()
	api.Handle(mux, api.ListContainers, func(r *http.Request, req api.ListContainersRequest) (api.ListContainersResponse, error) {
		if !req.RunningOnly {
			t.Error("ListRunning should set RunningOnly=true")
		}
		return api.ListContainersResponse{
			Containers: []api.ContainerInfo{
				{Name: "maestro-test-1", Status: "running"},
			},
			StateHash: "running-hash",
		}, nil
	})

	svc, ts := newTestDaemonService(t, mux)
	defer ts.Close()

	containers, err := svc.ListRunning(context.Background())
	if err != nil {
		t.Fatalf("ListRunning failed: %v", err)
	}
	if len(containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(containers))
	}
	if svc.StateHash() != "running-hash" {
		t.Errorf("expected state hash running-hash, got %s", svc.StateHash())
	}
}

func TestDaemonService_StopContainer(t *testing.T) {
	mux := http.NewServeMux()
	api.Handle(mux, api.StopContainer, func(r *http.Request, req api.StopContainerRequest) (api.StopContainerResponse, error) {
		if req.Name != "maestro-test-1" {
			t.Errorf("expected name maestro-test-1, got %s", req.Name)
		}
		if req.StateHash != "hash123" {
			t.Errorf("expected state hash hash123, got %s", req.StateHash)
		}
		return api.StopContainerResponse{Success: true, Message: "stopped"}, nil
	})

	svc, ts := newTestDaemonService(t, mux)
	defer ts.Close()

	err := svc.StopContainer(context.Background(), "maestro-test-1", "hash123")
	if err != nil {
		t.Fatalf("StopContainer failed: %v", err)
	}
}

func TestDaemonService_StopContainer_HashMismatch(t *testing.T) {
	mux := http.NewServeMux()
	api.Handle(mux, api.StopContainer, func(r *http.Request, req api.StopContainerRequest) (api.StopContainerResponse, error) {
		return api.StopContainerResponse{}, api.ErrStateHashMismatch
	})

	svc, ts := newTestDaemonService(t, mux)
	defer ts.Close()

	err := svc.StopContainer(context.Background(), "maestro-test-1", "stale-hash")
	if err == nil {
		t.Fatal("expected error for hash mismatch")
	}
	apiErr, ok := err.(*api.Error)
	if !ok {
		t.Fatalf("expected *api.Error, got %T", err)
	}
	if apiErr.Status != 409 {
		t.Errorf("expected 409, got %d", apiErr.Status)
	}
}

func TestDaemonService_CleanupContainers(t *testing.T) {
	mux := http.NewServeMux()
	api.Handle(mux, api.CleanupContainers, func(r *http.Request, req api.CleanupContainersRequest) (api.CleanupContainersResponse, error) {
		if len(req.Names) != 2 {
			t.Errorf("expected 2 names, got %d", len(req.Names))
		}
		return api.CleanupContainersResponse{
			Removed:        req.Names,
			VolumesRemoved: 6,
			Errors:         nil,
		}, nil
	})

	svc, ts := newTestDaemonService(t, mux)
	defer ts.Close()

	result, err := svc.CleanupContainers(context.Background(), []string{"a", "b"}, "hash")
	if err != nil {
		t.Fatalf("CleanupContainers failed: %v", err)
	}
	if len(result.Removed) != 2 {
		t.Errorf("expected 2 removed, got %d", len(result.Removed))
	}
	if result.VolumesRemoved != 6 {
		t.Errorf("expected 6 volumes removed, got %d", result.VolumesRemoved)
	}
}

func TestDaemonService_RefreshCache(t *testing.T) {
	mux := http.NewServeMux()
	api.Handle(mux, api.RefreshCache, func(r *http.Request, _ struct{}) (api.RefreshCacheResponse, error) {
		return api.RefreshCacheResponse{
			StateHash:   "refreshed-hash",
			RefreshedAt: time.Now(),
		}, nil
	})

	svc, ts := newTestDaemonService(t, mux)
	defer ts.Close()

	err := svc.RefreshCache(context.Background())
	if err != nil {
		t.Fatalf("RefreshCache failed: %v", err)
	}
	if svc.StateHash() != "refreshed-hash" {
		t.Errorf("expected state hash refreshed-hash, got %s", svc.StateHash())
	}
}

func TestDaemonService_IsDaemonConnected(t *testing.T) {
	svc := &daemonService{}
	if !svc.IsDaemonConnected() {
		t.Error("daemonService should always return true for IsDaemonConnected")
	}
}

func TestDockerService_IsDaemonConnected(t *testing.T) {
	svc := &dockerService{prefix: "test-"}
	if svc.IsDaemonConnected() {
		t.Error("dockerService should always return false for IsDaemonConnected")
	}
}

func TestDockerService_StateHash(t *testing.T) {
	svc := &dockerService{prefix: "test-"}
	if svc.StateHash() != "" {
		t.Error("dockerService StateHash should be empty")
	}
}

func TestDockerService_RefreshCache(t *testing.T) {
	svc := &dockerService{prefix: "test-"}
	if err := svc.RefreshCache(context.Background()); err != nil {
		t.Errorf("dockerService RefreshCache should be no-op, got: %v", err)
	}
}

func TestDockerService_Close(t *testing.T) {
	svc := &dockerService{prefix: "test-"}
	if err := svc.Close(); err != nil {
		t.Errorf("dockerService Close should be no-op, got: %v", err)
	}
}

func TestDaemonService_Close(t *testing.T) {
	svc := &daemonService{}
	if err := svc.Close(); err != nil {
		t.Errorf("daemonService Close should be no-op, got: %v", err)
	}
}

func TestNewDocker(t *testing.T) {
	svc := NewDocker("test-")
	if svc == nil {
		t.Fatal("NewDocker should not return nil")
	}
	if svc.IsDaemonConnected() {
		t.Error("docker service should not report daemon connected")
	}
}

func TestToContainerInfoSlice(t *testing.T) {
	now := time.Now()
	apiInfos := []api.ContainerInfo{
		{
			Name:          "maestro-test-1",
			ShortName:     "test-1",
			Status:        "running",
			StatusDetails: "Up 2 hours",
			Branch:        "feat/auth",
			AgentState:    "active",
			IsDormant:     false,
			AuthStatus:    "ok",
			LastActivity:  "2m ago",
			GitStatus:     "clean",
			CreatedAt:     now,
			CurrentTask:   "Implementing auth",
			TaskProgress:  "3/5",
		},
		{
			Name:      "maestro-test-2",
			ShortName: "test-2",
			Status:    "exited",
			IsDormant: true,
		},
	}

	result := toContainerInfoSlice(apiInfos)
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}

	// Verify all fields of first container
	c := result[0]
	if c.Name != "maestro-test-1" {
		t.Errorf("Name: got %s", c.Name)
	}
	if c.ShortName != "test-1" {
		t.Errorf("ShortName: got %s", c.ShortName)
	}
	if c.Status != "running" {
		t.Errorf("Status: got %s", c.Status)
	}
	if c.StatusDetails != "Up 2 hours" {
		t.Errorf("StatusDetails: got %s", c.StatusDetails)
	}
	if c.Branch != "feat/auth" {
		t.Errorf("Branch: got %s", c.Branch)
	}
	if c.AgentState != "active" {
		t.Errorf("AgentState: got %s", c.AgentState)
	}
	if c.IsDormant {
		t.Error("IsDormant should be false")
	}
	if c.AuthStatus != "ok" {
		t.Errorf("AuthStatus: got %s", c.AuthStatus)
	}
	if c.LastActivity != "2m ago" {
		t.Errorf("LastActivity: got %s", c.LastActivity)
	}
	if c.GitStatus != "clean" {
		t.Errorf("GitStatus: got %s", c.GitStatus)
	}
	if !c.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt: got %v", c.CreatedAt)
	}
	if c.CurrentTask != "Implementing auth" {
		t.Errorf("CurrentTask: got %s", c.CurrentTask)
	}
	if c.TaskProgress != "3/5" {
		t.Errorf("TaskProgress: got %s", c.TaskProgress)
	}

	// Second container
	c2 := result[1]
	if c2.Name != "maestro-test-2" {
		t.Errorf("Name: got %s", c2.Name)
	}
	if !c2.IsDormant {
		t.Error("IsDormant should be true for second container")
	}
}

func TestToContainerInfoSlice_Empty(t *testing.T) {
	result := toContainerInfoSlice([]api.ContainerInfo{})
	if len(result) != 0 {
		t.Errorf("expected 0 results, got %d", len(result))
	}
}

func TestToContainerInfoSlice_Nil(t *testing.T) {
	result := toContainerInfoSlice(nil)
	if len(result) != 0 {
		t.Errorf("expected 0 results, got %d", len(result))
	}
}

func TestCleanupResult_Fields(t *testing.T) {
	result := &CleanupResult{
		Removed:        []string{"a", "b"},
		VolumesRemoved: 4,
		Errors:         []string{"failed to remove c"},
	}
	if len(result.Removed) != 2 {
		t.Errorf("expected 2 removed, got %d", len(result.Removed))
	}
	if result.VolumesRemoved != 4 {
		t.Errorf("expected 4 volumes, got %d", result.VolumesRemoved)
	}
	if len(result.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(result.Errors))
	}
}

func TestDaemonService_StateHash_UpdatesOnList(t *testing.T) {
	callCount := 0
	mux := http.NewServeMux()
	api.Handle(mux, api.ListContainers, func(r *http.Request, req api.ListContainersRequest) (api.ListContainersResponse, error) {
		callCount++
		hash := "hash-v1"
		if callCount > 1 {
			hash = "hash-v2"
		}
		return api.ListContainersResponse{
			Containers: []api.ContainerInfo{},
			StateHash:  hash,
		}, nil
	})

	svc, ts := newTestDaemonService(t, mux)
	defer ts.Close()

	// First call
	svc.ListAll(context.Background())
	if svc.StateHash() != "hash-v1" {
		t.Errorf("expected hash-v1, got %s", svc.StateHash())
	}

	// Second call updates the hash
	svc.ListRunning(context.Background())
	if svc.StateHash() != "hash-v2" {
		t.Errorf("expected hash-v2, got %s", svc.StateHash())
	}
}

func TestDaemonService_AuthRejection(t *testing.T) {
	mux := http.NewServeMux()
	api.Handle(mux, api.ListContainers, func(r *http.Request, req api.ListContainersRequest) (api.ListContainersResponse, error) {
		if r.Header.Get("X-Maestro-Token") != "correct-token" {
			return api.ListContainersResponse{}, &api.Error{Status: 401, Message: "unauthorized"}
		}
		return api.ListContainersResponse{}, nil
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Client with wrong token
	client := &api.Client{
		BaseURL:    ts.URL,
		Token:      "wrong-token",
		HTTPClient: ts.Client(),
	}
	svc := &daemonService{client: client, prefix: "maestro-"}

	_, err := svc.ListAll(context.Background())
	if err == nil {
		t.Fatal("expected auth rejection error")
	}
	apiErr, ok := err.(*api.Error)
	if !ok {
		// The error is wrapped: "daemon list: 401: unauthorized"
		// Check it contains the right message
		if err.Error() == "" {
			t.Fatal("expected non-empty error")
		}
		return
	}
	if apiErr.Status != 401 {
		t.Errorf("expected 401, got %d", apiErr.Status)
	}
}

func TestNew_FallbackToDocker(t *testing.T) {
	// With a non-existent config dir, New should return a docker fallback
	svc, err := New("/nonexistent/path/that/does/not/exist", "maestro-")
	if err != nil {
		t.Fatalf("New should not error with missing config: %v", err)
	}
	if svc == nil {
		t.Fatal("New should return a service even without daemon")
	}
	if svc.IsDaemonConnected() {
		t.Error("should fall back to docker (not daemon connected)")
	}
}

// TestDaemonService_ListAll_ReturnsContainerInfo verifies the full round-trip
// from API types through toContainerInfoSlice back to container.Info.
func TestDaemonService_ListAll_ReturnsContainerInfo(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	mux := http.NewServeMux()
	api.Handle(mux, api.ListContainers, func(r *http.Request, req api.ListContainersRequest) (api.ListContainersResponse, error) {
		return api.ListContainersResponse{
			Containers: []api.ContainerInfo{
				{
					Name:       "maestro-test-1",
					ShortName:  "test-1",
					Status:     "running",
					Branch:     "main",
					IsDormant:  true,
					AuthStatus: "expired",
					CreatedAt:  now,
				},
			},
			StateHash: "h",
		}, nil
	})

	svc, ts := newTestDaemonService(t, mux)
	defer ts.Close()

	containers, err := svc.ListAll(context.Background())
	if err != nil {
		t.Fatalf("ListAll failed: %v", err)
	}

	// Verify the returned type is container.Info (not api.ContainerInfo)
	var _ []container.Info = containers
	c := containers[0]
	if c.Name != "maestro-test-1" {
		t.Errorf("expected maestro-test-1, got %s", c.Name)
	}
	if !c.IsDormant {
		t.Error("expected IsDormant=true")
	}
	if c.AuthStatus != "expired" {
		t.Errorf("expected AuthStatus=expired, got %s", c.AuthStatus)
	}
}
