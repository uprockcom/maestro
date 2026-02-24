package api

// StatusResponse is the response for GET /api/v1/status.
type StatusResponse struct {
	Running    bool     `json:"running"`
	PID        int      `json:"pid"`
	Containers []string `json:"containers"`
	Uptime     string   `json:"uptime"`
}

// ShutdownResponse is the response for POST /api/v1/shutdown.
type ShutdownResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}
