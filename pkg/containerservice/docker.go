package containerservice

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/uprockcom/maestro/pkg/container"
)

// dockerService calls Docker directly when the daemon is not running.
type dockerService struct {
	prefix string
}

func (s *dockerService) ListAll(ctx context.Context) ([]container.Info, error) {
	return container.GetAllContainers(s.prefix)
}

func (s *dockerService) ListRunning(ctx context.Context) ([]container.Info, error) {
	return container.GetRunningContainers(s.prefix)
}

func (s *dockerService) StopContainer(ctx context.Context, name string, stateHash string) error {
	// No state hash validation without daemon — just stop directly
	return container.StopContainer(name)
}

func (s *dockerService) CleanupContainers(ctx context.Context, names []string, stateHash string) (*CleanupResult, error) {
	result := &CleanupResult{}

	for _, name := range names {
		if err := container.DeleteContainer(name); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to remove %s: %v", name, err))
			continue
		}
		result.Removed = append(result.Removed, name)

		// Remove the claude-debug volume (not covered by container.DeleteContainer)
		vol := fmt.Sprintf("%s-claude-debug", name)
		volCmd := exec.Command("docker", "volume", "rm", vol)
		output, err := volCmd.CombinedOutput()
		if err == nil {
			result.VolumesRemoved++
		} else if !strings.Contains(string(output), "no such volume") {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to remove volume %s: %v", vol, err))
		}
	}

	return result, nil
}

func (s *dockerService) RefreshCache(ctx context.Context) error {
	return nil // no-op without daemon
}

func (s *dockerService) IsDaemonConnected() bool { return false }
func (s *dockerService) StateHash() string        { return "" }
func (s *dockerService) Close() error              { return nil }
