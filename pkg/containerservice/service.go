package containerservice

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/uprockcom/maestro/pkg/api"
	"github.com/uprockcom/maestro/pkg/container"
)

// CleanupResult holds the outcome of a cleanup operation.
type CleanupResult struct {
	Removed        []string
	VolumesRemoved int
	Errors         []string
}

// ContainerService abstracts container operations. When the daemon is running,
// operations go through the daemon's cache. When it's not, they fall back to
// direct Docker calls.
type ContainerService interface {
	ListAll(ctx context.Context) ([]container.Info, error)
	ListRunning(ctx context.Context) ([]container.Info, error)
	StopContainer(ctx context.Context, name string, stateHash string) error
	CleanupContainers(ctx context.Context, names []string, stateHash string) (*CleanupResult, error)
	RefreshCache(ctx context.Context) error
	IsDaemonConnected() bool
	StateHash() string
	Close() error
}

// New creates a ContainerService. If the daemon is running, returns a daemon-backed
// implementation. Otherwise returns a direct-Docker fallback.
func New(configDir string, prefix string) (ContainerService, error) {
	client, err := api.NewClientFromConfig(configDir)
	if err != nil {
		return nil, err
	}

	if client != nil {
		// Verify daemon is actually reachable (not stale daemon-ipc.json).
		// Use a short timeout so CLI startup isn't blocked by a hung daemon.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := api.Call(ctx, client, api.GetStatus, nil)
		if err != nil {
			// Daemon not actually running, fall back
			return NewDocker(prefix), nil
		}
		return &daemonService{client: client, prefix: prefix}, nil
	}

	return NewDocker(prefix), nil
}

// NewDocker creates a direct-Docker ContainerService (no daemon).
// Exported for use as explicit fallback.
func NewDocker(prefix string) ContainerService {
	return &dockerService{prefix: prefix}
}

// daemonService routes operations through the daemon's typed HTTP API.
type daemonService struct {
	client *api.Client
	prefix string

	mu            sync.Mutex
	lastStateHash string
}

func (s *daemonService) setStateHash(hash string) {
	s.mu.Lock()
	s.lastStateHash = hash
	s.mu.Unlock()
}

func (s *daemonService) ListAll(ctx context.Context) ([]container.Info, error) {
	resp, err := api.Call(ctx, s.client, api.ListContainers, &api.ListContainersRequest{
		RunningOnly: false,
	})
	if err != nil {
		return nil, fmt.Errorf("daemon list: %w", err)
	}
	s.setStateHash(resp.StateHash)
	return toContainerInfoSlice(resp.Containers), nil
}

func (s *daemonService) ListRunning(ctx context.Context) ([]container.Info, error) {
	resp, err := api.Call(ctx, s.client, api.ListContainers, &api.ListContainersRequest{
		RunningOnly: true,
	})
	if err != nil {
		return nil, fmt.Errorf("daemon list running: %w", err)
	}
	s.setStateHash(resp.StateHash)
	return toContainerInfoSlice(resp.Containers), nil
}

func (s *daemonService) StopContainer(ctx context.Context, name string, stateHash string) error {
	_, err := api.Call(ctx, s.client, api.StopContainer, &api.StopContainerRequest{
		Name:      name,
		StateHash: stateHash,
	})
	return err
}

func (s *daemonService) CleanupContainers(ctx context.Context, names []string, stateHash string) (*CleanupResult, error) {
	resp, err := api.Call(ctx, s.client, api.CleanupContainers, &api.CleanupContainersRequest{
		Names:     names,
		StateHash: stateHash,
	})
	if err != nil {
		return nil, err
	}
	return &CleanupResult{
		Removed:        resp.Removed,
		VolumesRemoved: resp.VolumesRemoved,
		Errors:         resp.Errors,
	}, nil
}

func (s *daemonService) RefreshCache(ctx context.Context) error {
	resp, err := api.Call(ctx, s.client, api.RefreshCache, nil)
	if err != nil {
		return err
	}
	s.setStateHash(resp.StateHash)
	return nil
}

func (s *daemonService) IsDaemonConnected() bool { return true }

func (s *daemonService) StateHash() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastStateHash
}

func (s *daemonService) Close() error { return nil }

// toContainerInfoSlice converts API types to internal container.Info types.
func toContainerInfoSlice(apiInfos []api.ContainerInfo) []container.Info {
	result := make([]container.Info, len(apiInfos))
	for i, a := range apiInfos {
		result[i] = container.Info{
			Name:          a.Name,
			ShortName:     a.ShortName,
			Status:        a.Status,
			StatusDetails: a.StatusDetails,
			Branch:        a.Branch,
			AgentState:    a.AgentState,
			IsDormant:     a.IsDormant,
			HasWeb:        a.HasWeb,
			AuthStatus:    a.AuthStatus,
			LastActivity:  a.LastActivity,
			GitStatus:     a.GitStatus,
			CreatedAt:     a.CreatedAt,
			CurrentTask:   a.CurrentTask,
			TaskProgress:  a.TaskProgress,
			Contacts:      a.Contacts,
		}
	}
	return result
}
