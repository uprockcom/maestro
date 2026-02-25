package daemon

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/uprockcom/maestro/pkg/api"
	"github.com/uprockcom/maestro/pkg/container"
	"github.com/uprockcom/maestro/pkg/notify"
)

// tokenAuthFunc returns an auth function for use with api.HandleWithAuth.
// Auth is checked at the HTTP level BEFORE body decoding.
func (s *IPCServer) tokenAuthFunc() func(r *http.Request) *api.Error {
	return func(r *http.Request) *api.Error {
		if r.Header.Get("X-Maestro-Token") != s.token {
			return &api.Error{Status: http.StatusUnauthorized, Message: "unauthorized"}
		}
		return nil
	}
}

func (s *IPCServer) handleListContainersV1(r *http.Request, req api.ListContainersRequest) (api.ListContainersResponse, error) {
	cachedBefore := s.daemon.containerCache.CachedAt()
	containers, hash, err := s.daemon.containerCache.Get()
	if err != nil {
		return api.ListContainersResponse{}, err
	}

	cachedAfter := s.daemon.containerCache.CachedAt()

	if req.RunningOnly {
		running := make([]api.ContainerInfo, 0)
		for _, c := range containers {
			if c.Status == "running" {
				running = append(running, c)
			}
		}
		containers = running
	}
	if containers == nil {
		containers = []api.ContainerInfo{}
	}

	return api.ListContainersResponse{
		Containers: containers,
		StateHash:  hash,
		CachedAt:   cachedAfter,
		FromCache:  cachedBefore.Equal(cachedAfter), // true if Get() used cache, false if refreshed
	}, nil
}

func (s *IPCServer) handleGetContainerV1(r *http.Request, _ struct{}) (api.ContainerInfo, error) {
	name := r.PathValue("name")
	if name == "" {
		return api.ContainerInfo{}, api.ErrNotFound
	}

	containers, _, err := s.daemon.containerCache.Get()
	if err != nil {
		return api.ContainerInfo{}, err
	}

	for _, c := range containers {
		if c.Name == name || c.ShortName == name {
			return c, nil
		}
	}

	return api.ContainerInfo{}, api.ErrNotFound
}

func (s *IPCServer) handleRefreshCacheV1(r *http.Request, _ struct{}) (api.RefreshCacheResponse, error) {
	containers, hash, err := s.daemon.containerCache.ForceRefresh()
	if err != nil {
		return api.RefreshCacheResponse{}, err
	}
	if containers == nil {
		containers = []api.ContainerInfo{}
	}
	return api.RefreshCacheResponse{
		Containers:  containers,
		StateHash:   hash,
		RefreshedAt: time.Now(),
	}, nil
}

func (s *IPCServer) handleStopContainerV1(r *http.Request, req api.StopContainerRequest) (api.StopContainerResponse, error) {
	if req.Name == "" {
		return api.StopContainerResponse{}, &api.Error{Status: 400, Message: "missing container name"}
	}
	// Ensure the container belongs to this maestro instance
	if !strings.HasPrefix(req.Name, s.daemon.config.ContainerPrefix) {
		return api.StopContainerResponse{}, &api.Error{Status: 403, Message: "container name does not match configured prefix"}
	}

	// Validate state hash (optimistic concurrency)
	if !s.daemon.containerCache.ValidateStateHash(req.StateHash) {
		return api.StopContainerResponse{}, api.ErrStateHashMismatch
	}

	if err := container.StopContainer(req.Name); err != nil {
		return api.StopContainerResponse{}, err
	}

	// Force cache refresh after mutation
	if _, _, err := s.daemon.containerCache.ForceRefresh(); err != nil {
		log.Printf("[WARN] cache refresh after stop %s: %v", req.Name, err)
	}

	return api.StopContainerResponse{
		Success: true,
		Message: fmt.Sprintf("container %s stopped", req.Name),
	}, nil
}

func (s *IPCServer) handleCleanupContainersV1(r *http.Request, req api.CleanupContainersRequest) (api.CleanupContainersResponse, error) {
	// Validate all container names belong to this maestro instance
	for _, name := range req.Names {
		if !strings.HasPrefix(name, s.daemon.config.ContainerPrefix) {
			return api.CleanupContainersResponse{}, &api.Error{
				Status:  403,
				Message: fmt.Sprintf("container %q does not match configured prefix", name),
			}
		}
	}

	// Validate state hash
	if !s.daemon.containerCache.ValidateStateHash(req.StateHash) {
		return api.CleanupContainersResponse{}, api.ErrStateHashMismatch
	}

	// Snapshot running state before mutations so we don't query mid-loop
	containers, _, err := s.daemon.containerCache.Get()
	if err != nil {
		return api.CleanupContainersResponse{}, &api.Error{
			Status:  http.StatusInternalServerError,
			Message: fmt.Sprintf("failed to get container state: %v", err),
		}
	}
	runningSet := make(map[string]bool)
	for _, c := range containers {
		if c.Status == "running" {
			runningSet[c.Name] = true
		}
	}

	removed := make([]string, 0)
	errors := make([]string, 0)
	totalVolumes := 0

	for _, name := range req.Names {
		// Stop if running (based on pre-loop snapshot)
		if runningSet[name] {
			if err := container.StopContainer(name); err != nil {
				errors = append(errors, fmt.Sprintf("failed to stop %s: %v", name, err))
				continue
			}
		}

		// Remove container
		if err := container.DeleteContainer(name); err != nil {
			errors = append(errors, fmt.Sprintf("failed to remove %s: %v", name, err))
			continue
		}
		removed = append(removed, name)

		// Remove claude-debug volume (npm/uv/history handled by DeleteContainer)
		vol := fmt.Sprintf("%s-claude-debug", name)
		if err := removeDockerVolume(vol); err == nil {
			totalVolumes++
		}
	}

	// Force cache refresh after mutations (unless caller will refresh separately)
	if !req.SkipRefresh {
		if _, _, err := s.daemon.containerCache.ForceRefresh(); err != nil {
			log.Printf("[WARN] cache refresh after cleanup: %v", err)
		}
	}

	return api.CleanupContainersResponse{
		Removed:        removed,
		VolumesRemoved: totalVolumes,
		Errors:         errors,
	}, nil
}

func (s *IPCServer) handleGetStatusV1(r *http.Request, _ struct{}) (api.StatusResponse, error) {
	containers, err := s.daemon.getRunningContainers()
	if err != nil || containers == nil {
		containers = []string{}
	}

	resp := api.StatusResponse{
		Running:    true,
		PID:        os.Getpid(),
		Containers: containers,
		Uptime:     time.Since(s.daemon.startTime).Truncate(time.Second).String(),
	}

	if result := s.daemon.UpdateStatus(); result != nil {
		resp.Update = &api.UpdateInfo{
			Available:      result.UpdateAvail,
			CurrentVersion: result.CurrentVersion,
			LatestVersion:  result.LatestVersion,
			ReleaseURL:     result.ReleaseURL,
		}
	}

	return resp, nil
}

func (s *IPCServer) handleGetPendingNotificationsV1(r *http.Request, _ struct{}) (api.ListPendingNotificationsResponse, error) {
	if s.daemon.localProvider == nil {
		return api.ListPendingNotificationsResponse{Questions: []api.PendingQuestion{}}, nil
	}

	pending := s.daemon.localProvider.GetPending()
	var questions []api.PendingQuestion
	for _, p := range pending {
		// Filter to only questions and container notifications (same as legacy handler)
		if p.Event.Type != notify.EventQuestion && p.Event.Type != notify.EventContainerNotification {
			continue
		}
		questions = append(questions, toAPIPendingQuestion(p))
	}

	if questions == nil {
		questions = []api.PendingQuestion{}
	}

	return api.ListPendingNotificationsResponse{Questions: questions}, nil
}

func (s *IPCServer) handleAnswerNotificationV1(r *http.Request, req api.AnswerNotificationRequest) (api.AnswerNotificationResponse, error) {
	if req.EventID == "" {
		return api.AnswerNotificationResponse{}, &api.Error{Status: 400, Message: "missing event_id"}
	}
	if s.daemon.localProvider == nil {
		return api.AnswerNotificationResponse{}, &api.Error{Status: 500, Message: "notification engine not configured"}
	}

	resp := notify.Response{
		EventID:    req.EventID,
		Selections: req.Selections,
		Text:       req.Text,
	}

	// Try direct local answer first, fall back to engine
	s.daemon.localProvider.Answer(req.EventID, resp)
	if s.daemon.notifyEngine != nil {
		s.daemon.notifyEngine.SubmitAnswer(req.EventID, resp)
	}

	return api.AnswerNotificationResponse{Success: true}, nil
}

func (s *IPCServer) handleDismissNotificationV1(r *http.Request, req api.DismissNotificationRequest) (api.DismissNotificationResponse, error) {
	if req.EventID == "" {
		return api.DismissNotificationResponse{}, &api.Error{Status: 400, Message: "missing event_id"}
	}
	if s.daemon.notifyEngine == nil {
		return api.DismissNotificationResponse{}, &api.Error{Status: 500, Message: "notification engine not configured"}
	}

	s.daemon.notifyEngine.CancelQuestionWithNotify(req.EventID)
	return api.DismissNotificationResponse{Success: true}, nil
}

func (s *IPCServer) handleShutdownV1(r *http.Request, _ struct{}) (api.ShutdownResponse, error) {
	go func() {
		time.Sleep(100 * time.Millisecond)
		s.daemon.Stop()
	}()
	return api.ShutdownResponse{
		Success: true,
		Message: "shutting down",
	}, nil
}

// removeDockerVolume removes a Docker volume by name.
// Returns nil on success, error on failure (including "no such volume").
func removeDockerVolume(name string) error {
	cmd := exec.Command("docker", "volume", "rm", name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(output), "no such volume") {
			return fmt.Errorf("no such volume: %s", name)
		}
		return fmt.Errorf("failed to remove volume %s: %w", name, err)
	}
	return nil
}

// toAPIPendingQuestion converts a notify.PendingQuestion to an api.PendingQuestion.
func toAPIPendingQuestion(p notify.PendingQuestion) api.PendingQuestion {
	var question *api.QuestionData
	if p.Event.Question != nil {
		items := make([]api.QuestionItem, len(p.Event.Question.Questions))
		for i, q := range p.Event.Question.Questions {
			opts := make([]api.QuestionOption, len(q.Options))
			for j, o := range q.Options {
				opts[j] = api.QuestionOption{
					Label:       o.Label,
					Description: o.Description,
				}
			}
			items[i] = api.QuestionItem{
				Question:    q.Question,
				Header:      q.Header,
				Options:     opts,
				MultiSelect: q.MultiSelect,
			}
		}
		question = &api.QuestionData{Questions: items}
	}

	return api.PendingQuestion{
		Event: api.Event{
			ID:            p.Event.ID,
			ContainerName: p.Event.ContainerName,
			ShortName:     p.Event.ShortName,
			Branch:        p.Event.Branch,
			Title:         p.Event.Title,
			Message:       p.Event.Message,
			Type:          api.EventType(p.Event.Type),
			Timestamp:     p.Event.Timestamp,
			Question:      question,
		},
		SentAt: p.SentAt,
	}
}
