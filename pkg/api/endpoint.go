package api

import "strings"

// Endpoint binds an HTTP route pattern to request and response types at compile time.
// Both server and client code references the same Endpoint variable, so if the
// request or response type changes, the compiler catches it on both sides.
type Endpoint[Req, Resp any] struct {
	Pattern string // Go 1.22+ mux pattern: "GET /api/v1/containers"
	Method  string // "GET", "POST"
	Path    string // "/api/v1/containers"
}

// NewEndpoint creates a typed endpoint descriptor from a Go 1.22+ mux pattern.
// Example: NewEndpoint[ListContainersRequest, ListContainersResponse]("GET /api/v1/containers")
func NewEndpoint[Req, Resp any](pattern string) Endpoint[Req, Resp] {
	parts := strings.SplitN(pattern, " ", 2)
	if len(parts) != 2 {
		panic("api: invalid endpoint pattern: " + pattern)
	}
	return Endpoint[Req, Resp]{
		Pattern: pattern,
		Method:  parts[0],
		Path:    parts[1],
	}
}

// Route registry — single source of truth for route + types.
var (
	ListContainers          = NewEndpoint[ListContainersRequest, ListContainersResponse]("GET /api/v1/containers")
	GetContainer            = NewEndpoint[struct{}, ContainerInfo]("GET /api/v1/containers/{name}")
	RefreshCache            = NewEndpoint[struct{}, RefreshCacheResponse]("POST /api/v1/containers/refresh")
	StopContainer           = NewEndpoint[StopContainerRequest, StopContainerResponse]("POST /api/v1/containers/stop")
	CleanupContainers       = NewEndpoint[CleanupContainersRequest, CleanupContainersResponse]("POST /api/v1/containers/cleanup")
	GetStatus               = NewEndpoint[struct{}, StatusResponse]("GET /api/v1/status")
	GetPendingNotifications = NewEndpoint[struct{}, ListPendingNotificationsResponse]("GET /api/v1/notifications/pending")
	AnswerNotification      = NewEndpoint[AnswerNotificationRequest, AnswerNotificationResponse]("POST /api/v1/notifications/answer")
	DismissNotification     = NewEndpoint[DismissNotificationRequest, DismissNotificationResponse]("POST /api/v1/notifications/dismiss")
	Shutdown                = NewEndpoint[struct{}, ShutdownResponse]("POST /api/v1/shutdown")
)
