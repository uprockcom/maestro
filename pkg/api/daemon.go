package api

// StatusResponse is the response for GET /api/v1/status.
type StatusResponse struct {
	Running    bool        `json:"running"`
	PID        int         `json:"pid"`
	Containers []string    `json:"containers"`
	Uptime     string      `json:"uptime"`
	Update     *UpdateInfo `json:"update,omitempty"`
}

// UpdateInfo conveys whether a newer version is available.
type UpdateInfo struct {
	Available      bool   `json:"available"`
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	ReleaseURL     string `json:"release_url,omitempty"`
}

// ShutdownResponse is the response for POST /api/v1/shutdown.
type ShutdownResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}
