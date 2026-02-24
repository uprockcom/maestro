package api

import "time"

// ContainerInfo is the wire type for container data over the API.
// This mirrors container.Info exactly so the daemon can serialize its cache
// and clients can deserialize without importing pkg/container.
//
// IMPORTANT: When adding fields here, also add them to container.Info
// in pkg/container/types.go, and update the conversion functions in
// pkg/daemon/container_cache.go and pkg/containerservice/service.go.
type ContainerInfo struct {
	Name          string    `json:"name"`
	ShortName     string    `json:"short_name"`
	Status        string    `json:"status"`
	StatusDetails string    `json:"status_details,omitempty"`
	Branch        string    `json:"branch,omitempty"`
	AgentState    string    `json:"agent_state,omitempty"`
	IsDormant     bool      `json:"is_dormant"`
	HasWeb        bool      `json:"has_web"`
	AuthStatus    string    `json:"auth_status,omitempty"`
	LastActivity  string    `json:"last_activity,omitempty"`
	GitStatus     string    `json:"git_status,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	CurrentTask   string                       `json:"current_task,omitempty"`
	TaskProgress  string                       `json:"task_progress,omitempty"`
	Contacts      map[string]map[string]string `json:"contacts,omitempty"`
}

// ListContainersRequest is the request for GET /api/v1/containers.
type ListContainersRequest struct {
	RunningOnly bool `json:"running_only,omitempty"`
}

// ListContainersResponse is the response for GET /api/v1/containers.
type ListContainersResponse struct {
	Containers []ContainerInfo `json:"containers"`
	StateHash  string          `json:"state_hash"`
	CachedAt   time.Time       `json:"cached_at"`
	FromCache  bool            `json:"from_cache"`
}

// RefreshCacheResponse is the response for POST /api/v1/containers/refresh.
type RefreshCacheResponse struct {
	Containers  []ContainerInfo `json:"containers"`
	StateHash   string          `json:"state_hash"`
	RefreshedAt time.Time       `json:"refreshed_at"`
}

// StopContainerRequest is the request for POST /api/v1/containers/stop.
type StopContainerRequest struct {
	Name      string `json:"name"`
	StateHash string `json:"state_hash"`
}

// StopContainerResponse is the response for POST /api/v1/containers/stop.
type StopContainerResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// CleanupContainersRequest is the request for POST /api/v1/containers/cleanup.
type CleanupContainersRequest struct {
	Names     []string `json:"names"`
	StateHash string   `json:"state_hash"`
}

// CleanupContainersResponse is the response for POST /api/v1/containers/cleanup.
type CleanupContainersResponse struct {
	Removed        []string `json:"removed"`
	VolumesRemoved int      `json:"volumes_removed"`
	Errors         []string `json:"errors,omitempty"`
}
