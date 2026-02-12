// Copyright 2025 Christopher O'Connell
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/uprockcom/maestro/pkg/container"
)

// validRequestID matches UUID v4 format used for IPC request IDs
var validRequestID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

// childInfo tracks a child container's parent relationship
type childInfo struct {
	Parent    string
	RequestID string
}

// IPCServer handles HTTP requests from containers and CLI over TCP
type IPCServer struct {
	daemon         *Daemon
	listeners      []net.Listener
	server         *http.Server
	token          string
	loopbackPort   int
	bridgePort     int
	inFlightMu     sync.Mutex          // protects inFlight
	inFlight       map[string]bool      // request IDs currently being processed (dedup recovery)
	childParentsMu sync.Mutex          // protects childParents
	childParents   map[string]childInfo // child container name → parent info
}

// NewIPCServer creates a new IPC server with a loopback listener (and optionally a Docker bridge listener)
func NewIPCServer(d *Daemon, token string) (*IPCServer, error) {
	// Always bind loopback for CLI access from host
	loopback, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to listen on loopback: %w", err)
	}

	s := &IPCServer{
		daemon:       d,
		listeners:    []net.Listener{loopback},
		token:        token,
		loopbackPort: loopback.Addr().(*net.TCPAddr).Port,
		inFlight:     make(map[string]bool),
		childParents: make(map[string]childInfo),
	}

	// On Linux, also bind to the Docker bridge IP so containers can reach us directly
	if bridgeIP := detectDockerBridgeIP(); bridgeIP != "" {
		bridge, err := net.Listen("tcp", bridgeIP+":0")
		if err != nil {
			d.logInfo("Could not bind Docker bridge IP %s: %v (containers will use loopback via host.docker.internal)", bridgeIP, err)
		} else {
			s.listeners = append(s.listeners, bridge)
			s.bridgePort = bridge.Addr().(*net.TCPAddr).Port
			d.logInfo("Also listening on Docker bridge %s:%d", bridgeIP, s.bridgePort)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /request", s.requireAuth(s.handleRequest))
	mux.HandleFunc("GET /status", s.handleStatus)
	mux.HandleFunc("POST /shutdown", s.requireAuth(s.handleShutdown))

	s.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s, nil
}

// detectDockerBridgeIP returns the first IPv4 address on the docker0 interface (Linux only).
// On macOS/Windows, Docker Desktop routes host.docker.internal to localhost automatically,
// so no bridge binding is needed and this returns "".
func detectDockerBridgeIP() string {
	iface, err := net.InterfaceByName("docker0")
	if err != nil {
		return ""
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() != nil {
			return ipNet.IP.String()
		}
	}
	return ""
}

// requireAuth wraps a handler to require the X-Maestro-Token header
func (s *IPCServer) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Maestro-Token") != s.token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// Start begins serving requests — one goroutine per listener
func (s *IPCServer) Start() {
	for _, ln := range s.listeners {
		go func(l net.Listener) {
			if err := s.server.Serve(l); err != nil && err != http.ErrServerClosed {
				s.daemon.logError("IPC server error: %v", err)
			}
		}(ln)
	}
	s.daemon.logInfo("IPC server started on 127.0.0.1:%d", s.loopbackPort)
}

// Stop gracefully shuts down the IPC server (closes all listeners)
func (s *IPCServer) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.server.Shutdown(ctx); err != nil {
		s.daemon.logError("IPC server shutdown error: %v", err)
	}

	s.daemon.logInfo("IPC server stopped")
}

// LoopbackPort returns the TCP port on 127.0.0.1 (for CLI / daemon-ipc.json)
func (s *IPCServer) LoopbackPort() int {
	return s.loopbackPort
}

// BridgePort returns the TCP port on the Docker bridge interface, or 0 if not bound
func (s *IPCServer) BridgePort() int {
	return s.bridgePort
}

func (s *IPCServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	var req IPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, IPCResponse{
			Status: "error",
			Error:  "invalid JSON: " + err.Error(),
		})
		return
	}

	if req.ID == "" || req.Parent == "" {
		writeJSON(w, http.StatusBadRequest, IPCResponse{
			Status: "error",
			Error:  "missing required fields: id, parent",
		})
		return
	}

	// Validate container name prefix to prevent spoofing
	if !strings.HasPrefix(req.Parent, s.daemon.config.ContainerPrefix) {
		writeJSON(w, http.StatusBadRequest, IPCResponse{
			Status: "error",
			Error:  "invalid parent container name",
		})
		return
	}

	switch req.Action {
	case IPCActionNew:
		s.handleNewContainer(w, req)
	case IPCActionNotify:
		s.handleNotify(w, req)
	case IPCActionExit:
		s.handleExit(w, req)
	case IPCActionWaitIdle:
		s.handleWaitIdle(w, req)
	case IPCActionReadMessages:
		s.handleReadMessages(w, req)
	case IPCActionSendMessage:
		s.handleSendMessage(w, req)
	default:
		writeJSON(w, http.StatusBadRequest, IPCResponse{
			Status: "error",
			Error:  fmt.Sprintf("unknown action: %s", req.Action),
		})
	}
}

func (s *IPCServer) handleNewContainer(w http.ResponseWriter, req IPCRequest) {
	if req.Task == "" {
		writeJSON(w, http.StatusBadRequest, IPCResponse{
			Status: "error",
			Error:  "missing required field: task",
		})
		return
	}

	s.daemon.logInfo("IPC: new container request from %s: %s", req.Parent, req.Task)

	// Return 202 Accepted immediately, process in background
	writeJSON(w, http.StatusAccepted, IPCResponse{
		Status: "accepted",
	})

	// Process in background
	s.daemon.wg.Add(1)
	go func() {
		defer s.daemon.wg.Done()
		childName, err := s.daemon.createChildContainer(req.Task, req.Parent, req.Branch)
		if err != nil {
			s.daemon.logError("IPC: failed to create child container for %s: %v", req.Parent, err)
			errMsg := err.Error()
			s.updateRequestFile(req.Parent, req.ID, IPCRequestStatusFailed, "", errMsg)
		} else {
			s.daemon.logInfo("IPC: created child container %s for %s", childName, req.Parent)
			s.updateRequestFile(req.Parent, req.ID, IPCRequestStatusFulfilled, childName, "")
			s.childParentsMu.Lock()
			s.childParents[childName] = childInfo{Parent: req.Parent, RequestID: req.ID}
			s.childParentsMu.Unlock()
		}
	}()
}

func (s *IPCServer) handleNotify(w http.ResponseWriter, req IPCRequest) {
	if req.Title == "" || req.Message == "" {
		writeJSON(w, http.StatusBadRequest, IPCResponse{
			Status: "error",
			Error:  "missing required fields: title, message",
		})
		return
	}

	containerShort := s.daemon.getShortName(req.Parent)

	// Container notifications are always delivered — the agent explicitly asked to
	// notify the user. The shouldNotify rate-limiting only applies to daemon-generated
	// alerts (attention_needed, token_expiring), not explicit IPC requests.
	if s.daemon.config.NotificationsOn && !s.daemon.isQuietHours() {
		s.daemon.notify(req.Title, containerShort, req.Message)
	}

	s.daemon.logInfo("IPC: notification from %s: %s - %s", containerShort, req.Title, req.Message)

	writeJSON(w, http.StatusOK, IPCResponse{
		Status: "ok",
	})
}

func (s *IPCServer) handleExit(w http.ResponseWriter, req IPCRequest) {
	s.daemon.logInfo("IPC: exit request from %s", req.Parent)

	// Return 200 OK immediately, then stop the container
	writeJSON(w, http.StatusOK, IPCResponse{
		Status: "ok",
	})

	s.daemon.wg.Add(1)
	go func() {
		defer s.daemon.wg.Done()
		// Give the response a moment to be sent back to the container
		time.Sleep(1 * time.Second)
		s.stopContainerAndNotify(req.Parent)
	}()
}

func (s *IPCServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	containers, err := s.daemon.getRunningContainers()
	if err != nil {
		containers = []string{}
	}

	writeJSON(w, http.StatusOK, IPCStatusResponse{
		Running:    true,
		PID:        os.Getpid(),
		Containers: containers,
		Uptime:     s.daemon.getUptime(),
	})
}

func (s *IPCServer) handleShutdown(w http.ResponseWriter, r *http.Request) {
	s.daemon.logInfo("IPC: shutdown request received")

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "shutting_down",
	})

	// Stop the daemon after a short delay to flush the response
	go func() {
		time.Sleep(100 * time.Millisecond)
		s.daemon.Stop()
	}()
}

// stopContainerAndNotify stops a container and immediately checks for child
// exit notifications. This ensures parents are notified promptly regardless
// of how a container is stopped (exit request, recovery, etc.), rather than
// waiting for the next periodic check() cycle.
func (s *IPCServer) stopContainerAndNotify(containerName string) {
	stopCmd := exec.Command("docker", "stop", containerName)
	if err := stopCmd.Run(); err != nil {
		s.daemon.logError("IPC: failed to stop container %s: %v", containerName, err)
		return
	}
	s.daemon.logInfo("IPC: stopped container %s", containerName)

	// Get fresh container list and notify parents of any stopped children
	containers, err := s.daemon.getRunningContainers()
	if err != nil {
		s.daemon.logError("IPC: failed to get running containers after stop: %v", err)
		return
	}
	s.notifyStoppedChildren(containers)
}

// updateRequestFile updates a request file in a container with the result
func (s *IPCServer) updateRequestFile(containerName, requestID string, status IPCRequestStatus, childContainer, errMsg string) {
	// Validate request ID to prevent path traversal
	if !validRequestID.MatchString(requestID) {
		s.daemon.logError("IPC: invalid request ID format: %s", requestID)
		return
	}

	requestPath := fmt.Sprintf("/home/node/.maestro/requests/%s.json", requestID)

	// Read current file from container
	readCmd := exec.Command("docker", "exec", containerName, "cat", requestPath)
	output, err := readCmd.Output()
	if err != nil {
		s.daemon.logError("IPC: failed to read request file %s in %s: %v", requestID, containerName, err)
		return
	}

	var reqFile IPCRequestFile
	if err := json.Unmarshal(output, &reqFile); err != nil {
		s.daemon.logError("IPC: failed to parse request file %s: %v", requestID, err)
		return
	}

	// Update fields
	reqFile.Status = status
	now := time.Now()
	if status == IPCRequestStatusChildExited {
		reqFile.ChildExitedAt = &now
	} else {
		reqFile.FulfilledAt = &now
	}
	if childContainer != "" {
		reqFile.ChildContainer = &childContainer
	}
	if errMsg != "" {
		reqFile.Error = &errMsg
	}

	// Write back to container
	updatedJSON, err := json.MarshalIndent(reqFile, "", "  ")
	if err != nil {
		s.daemon.logError("IPC: failed to marshal updated request file: %v", err)
		return
	}

	writeCmd := exec.Command("docker", "exec", "-i", containerName, "tee", requestPath)
	writeCmd.Stdin = strings.NewReader(string(updatedJSON))
	writeCmd.Stdout = nil // suppress tee's stdout echo
	if err := writeCmd.Run(); err != nil {
		s.daemon.logError("IPC: failed to write updated request file %s in %s: %v", requestID, containerName, err)
	}
}

// notifyStoppedChildren checks if any tracked child containers have stopped
// and updates their parent's request file to child_exited status.
func (s *IPCServer) notifyStoppedChildren(activeContainers []string) {
	active := make(map[string]bool, len(activeContainers))
	for _, c := range activeContainers {
		active[c] = true
	}

	// Collect stopped children under the lock
	s.childParentsMu.Lock()
	var stopped []struct {
		child string
		info  childInfo
	}
	for child, info := range s.childParents {
		if !active[child] {
			stopped = append(stopped, struct {
				child string
				info  childInfo
			}{child, info})
			delete(s.childParents, child)
		}
	}
	s.childParentsMu.Unlock()

	// Update parent request files for each stopped child
	for _, entry := range stopped {
		// Only update if the parent is still running
		if !active[entry.info.Parent] {
			s.daemon.logInfo("IPC: child %s stopped but parent %s is also gone, skipping notification",
				entry.child, entry.info.Parent)
			continue
		}
		s.daemon.logInfo("IPC: child %s stopped, notifying parent %s (request %s)",
			entry.child, entry.info.Parent, entry.info.RequestID)
		s.updateRequestFile(entry.info.Parent, entry.info.RequestID, IPCRequestStatusChildExited, "", "")
	}
}

// checkPendingRequests scans a container for pending IPC requests and processes them.
// Called from the daemon check loop for recovery after restart.
func (s *IPCServer) checkPendingRequests(containerName string, state *ContainerState) {
	// Don't check too frequently (every 30 seconds)
	state.mu.Lock()
	if time.Since(state.LastIPCCheck) < 30*time.Second {
		state.mu.Unlock()
		return
	}
	state.LastIPCCheck = time.Now()
	state.mu.Unlock()

	// List request files in the container
	listCmd := exec.Command("docker", "exec", containerName, "find",
		"/home/node/.maestro/requests", "-name", "*.json", "-type", "f")
	output, err := listCmd.Output()
	if err != nil {
		// Directory might not exist - that's fine
		return
	}

	files := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, filePath := range files {
		filePath = strings.TrimSpace(filePath)
		if filePath == "" {
			continue
		}

		// Read the request file
		readCmd := exec.Command("docker", "exec", containerName, "cat", filePath)
		fileContent, err := readCmd.Output()
		if err != nil {
			continue
		}

		var reqFile IPCRequestFile
		if err := json.Unmarshal(fileContent, &reqFile); err != nil {
			continue
		}

		// Rebuild child tracking for fulfilled "new" requests (daemon restart recovery)
		if reqFile.Status == IPCRequestStatusFulfilled && reqFile.Action == IPCActionNew && reqFile.ChildContainer != nil {
			childName := *reqFile.ChildContainer
			s.childParentsMu.Lock()
			_, alreadyTracked := s.childParents[childName]
			s.childParentsMu.Unlock()
			if !alreadyTracked {
				// Check if the child container is still running
				checkCmd := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", childName)
				if out, err := checkCmd.Output(); err == nil && strings.TrimSpace(string(out)) == "true" {
					// Child still running — re-register for live tracking
					s.childParentsMu.Lock()
					s.childParents[childName] = childInfo{Parent: containerName, RequestID: reqFile.ID}
					s.childParentsMu.Unlock()
					s.daemon.logInfo("IPC: recovered child tracking: %s → parent %s (request %s)",
						childName, containerName, reqFile.ID)
				} else {
					// Child already stopped — notify parent immediately
					s.daemon.logInfo("IPC: child %s already stopped on recovery, notifying parent %s",
						childName, containerName)
					s.updateRequestFile(containerName, reqFile.ID, IPCRequestStatusChildExited, "", "")
				}
			}
			continue
		}

		// Only process pending requests
		if reqFile.Status != IPCRequestStatusPending {
			continue
		}

		// Skip stale requests (>24h old)
		if time.Since(reqFile.RequestedAt) > 24*time.Hour {
			s.daemon.logInfo("IPC: skipping stale request %s in %s (age: %s)",
				reqFile.ID, containerName, time.Since(reqFile.RequestedAt).Round(time.Minute))
			continue
		}

		// Override parent with the trusted container name from the daemon.
		// The request file lives inside the container and could be tampered with;
		// the daemon knows which container it read the file from.
		if reqFile.Parent != containerName {
			s.daemon.logInfo("IPC: request %s claimed parent=%q but was found in %s — overriding",
				reqFile.ID, reqFile.Parent, containerName)
			reqFile.Parent = containerName
		}

		// Deduplicate: skip if this request is already being processed
		s.inFlightMu.Lock()
		if s.inFlight[reqFile.ID] {
			s.inFlightMu.Unlock()
			continue
		}
		s.inFlight[reqFile.ID] = true
		s.inFlightMu.Unlock()

		s.daemon.logInfo("IPC: recovering pending request %s (%s) from %s",
			reqFile.ID, reqFile.Action, containerName)

		switch reqFile.Action {
		case IPCActionNew:
			s.daemon.wg.Add(1)
			go func(rf IPCRequestFile) {
				defer s.daemon.wg.Done()
				defer func() {
					s.inFlightMu.Lock()
					delete(s.inFlight, rf.ID)
					s.inFlightMu.Unlock()
				}()
				childName, err := s.daemon.createChildContainer(rf.Task, rf.Parent, rf.Branch)
				if err != nil {
					s.daemon.logError("IPC: recovery failed for %s: %v", rf.ID, err)
					errMsg := err.Error()
					s.updateRequestFile(containerName, rf.ID, IPCRequestStatusFailed, "", errMsg)
				} else {
					s.daemon.logInfo("IPC: recovery created child %s for %s", childName, containerName)
					s.updateRequestFile(containerName, rf.ID, IPCRequestStatusFulfilled, childName, "")
					s.childParentsMu.Lock()
					s.childParents[childName] = childInfo{Parent: containerName, RequestID: rf.ID}
					s.childParentsMu.Unlock()
				}
			}(reqFile)

		case IPCActionNotify:
			containerShort := s.daemon.getShortName(containerName)
			if s.daemon.config.NotificationsOn && !s.daemon.isQuietHours() {
				s.daemon.notify(reqFile.Title, containerShort, reqFile.Message)
			}
			s.updateRequestFile(containerName, reqFile.ID, IPCRequestStatusFulfilled, "", "")
			s.inFlightMu.Lock()
			delete(s.inFlight, reqFile.ID)
			s.inFlightMu.Unlock()

		case IPCActionExit:
			s.daemon.wg.Add(1)
			go func(rf IPCRequestFile) {
				defer s.daemon.wg.Done()
				defer func() {
					s.inFlightMu.Lock()
					delete(s.inFlight, rf.ID)
					s.inFlightMu.Unlock()
				}()
				s.stopContainerAndNotify(containerName)
			}(reqFile)

		case IPCActionWaitIdle:
			// Restart polling goroutine on recovery
			s.daemon.wg.Add(1)
			go func(rf IPCRequestFile) {
				defer s.daemon.wg.Done()
				defer func() {
					s.inFlightMu.Lock()
					delete(s.inFlight, rf.ID)
					s.inFlightMu.Unlock()
				}()
				timeout := rf.Timeout
				if timeout <= 0 {
					timeout = 300
				}
				fakeReq := IPCRequest{
					ID:              rf.ID,
					Action:          rf.Action,
					Parent:          containerName,
					TargetRequestID: rf.TargetRequestID,
					Timeout:         timeout,
				}
				childContainer, err := s.resolveChildContainer(fakeReq)
				if err != nil {
					s.daemon.logError("IPC: recovery failed for wait_idle %s: %v", rf.ID, err)
					errMsg := err.Error()
					s.updateRequestFile(containerName, rf.ID, IPCRequestStatusFailed, "", errMsg)
					return
				}
				s.daemon.logInfo("IPC: recovering wait_idle %s for child %s", rf.ID, childContainer)

				deadline := time.After(time.Duration(timeout) * time.Second)
				ticker := time.NewTicker(2 * time.Second)
				defer ticker.Stop()

				for {
					select {
					case <-s.daemon.stopChan:
						s.updateRequestFile(containerName, rf.ID, IPCRequestStatusFailed, "", "daemon shutting down")
						return
					case <-deadline:
						errMsg := fmt.Sprintf("timeout: child did not reach idle state within %ds", timeout)
						s.updateRequestFile(containerName, rf.ID, IPCRequestStatusFailed, "", errMsg)
						return
					case <-ticker.C:
						checkCmd := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", childContainer)
						out, err := checkCmd.Output()
						if err != nil || strings.TrimSpace(string(out)) != "true" {
							s.updateRequestFile(containerName, rf.ID, IPCRequestStatusFailed, "", "child container exited")
							return
						}
						if !container.IsClaudeRunning(childContainer) {
							s.updateRequestFile(containerName, rf.ID, IPCRequestStatusFailed, "", "claude process exited (dormant)")
							return
						}
						if container.CheckIdleStatus(childContainer) {
							s.updateRequestFile(containerName, rf.ID, IPCRequestStatusFulfilled, "", "")
							return
						}
					}
				}
			}(reqFile)

		case IPCActionReadMessages:
			// Re-execute (idempotent read)
			s.daemon.wg.Add(1)
			go func(rf IPCRequestFile) {
				defer s.daemon.wg.Done()
				defer func() {
					s.inFlightMu.Lock()
					delete(s.inFlight, rf.ID)
					s.inFlightMu.Unlock()
				}()
				count := rf.Count
				if count <= 0 {
					count = 10
				}
				if count > 50 {
					count = 50
				}
				fakeReq := IPCRequest{
					ID:              rf.ID,
					Action:          rf.Action,
					Parent:          containerName,
					TargetRequestID: rf.TargetRequestID,
				}
				childContainer, err := s.resolveChildContainer(fakeReq)
				if err != nil {
					s.daemon.logError("IPC: recovery failed for read_messages %s: %v", rf.ID, err)
					errMsg := err.Error()
					s.updateRequestFile(containerName, rf.ID, IPCRequestStatusFailed, "", errMsg)
					return
				}
				messages, err := s.readClaudeMessages(childContainer, count)
				if err != nil {
					errMsg := err.Error()
					s.updateRequestFile(containerName, rf.ID, IPCRequestStatusFailed, "", errMsg)
					return
				}
				s.updateRequestFileWithMessages(containerName, rf.ID, messages)
			}(reqFile)

		case IPCActionSendMessage:
			// Not safe to resend — mark as failed
			errMsg := "daemon restarted: send_message not retried (not idempotent)"
			s.updateRequestFile(containerName, reqFile.ID, IPCRequestStatusFailed, "", errMsg)
			s.inFlightMu.Lock()
			delete(s.inFlight, reqFile.ID)
			s.inFlightMu.Unlock()
		}
	}
}

// resolveChildContainer authenticates a parent→child request by reading the original
// "new" request from the parent's filesystem and validating ownership.
func (s *IPCServer) resolveChildContainer(req IPCRequest) (string, error) {
	if req.TargetRequestID == "" {
		return "", fmt.Errorf("missing target_request_id")
	}
	if !validRequestID.MatchString(req.TargetRequestID) {
		return "", fmt.Errorf("invalid target_request_id format")
	}

	// Read the original "new" request from the parent's filesystem
	path := fmt.Sprintf("/home/node/.maestro/requests/%s.json", req.TargetRequestID)
	readCmd := exec.Command("docker", "exec", req.Parent, "cat", path)
	output, err := readCmd.Output()
	if err != nil {
		return "", fmt.Errorf("request %s not found in parent container", req.TargetRequestID)
	}

	var origReq IPCRequestFile
	if err := json.Unmarshal(output, &origReq); err != nil {
		return "", fmt.Errorf("invalid request file")
	}

	// Auth checks
	if origReq.Parent != req.Parent {
		return "", fmt.Errorf("unauthorized: not the parent of this request")
	}
	if origReq.Action != IPCActionNew {
		return "", fmt.Errorf("target request is not a child creation request")
	}
	if origReq.ChildContainer == nil {
		return "", fmt.Errorf("child container not yet created (request still pending)")
	}

	return *origReq.ChildContainer, nil
}

func (s *IPCServer) handleWaitIdle(w http.ResponseWriter, req IPCRequest) {
	childContainer, err := s.resolveChildContainer(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, IPCResponse{
			Status: "error",
			Error:  err.Error(),
		})
		return
	}

	s.daemon.logInfo("IPC: wait_idle request from %s for child %s", req.Parent, childContainer)

	writeJSON(w, http.StatusAccepted, IPCResponse{
		Status: "accepted",
	})

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 300
	}

	s.daemon.wg.Add(1)
	go func() {
		defer s.daemon.wg.Done()

		deadline := time.After(time.Duration(timeout) * time.Second)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-s.daemon.stopChan:
				s.updateRequestFile(req.Parent, req.ID, IPCRequestStatusFailed, "", "daemon shutting down")
				return
			case <-deadline:
				errMsg := fmt.Sprintf("timeout: child did not reach idle state within %ds", timeout)
				s.updateRequestFile(req.Parent, req.ID, IPCRequestStatusFailed, "", errMsg)
				return
			case <-ticker.C:
				// Check if child container is still running
				checkCmd := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", childContainer)
				out, err := checkCmd.Output()
				if err != nil || strings.TrimSpace(string(out)) != "true" {
					s.updateRequestFile(req.Parent, req.ID, IPCRequestStatusFailed, "", "child container exited")
					return
				}

				// Check if Claude is running
				if !container.IsClaudeRunning(childContainer) {
					s.updateRequestFile(req.Parent, req.ID, IPCRequestStatusFailed, "", "claude process exited (dormant)")
					return
				}

				// Check if idle
				if container.CheckIdleStatus(childContainer) {
					s.updateRequestFile(req.Parent, req.ID, IPCRequestStatusFulfilled, "", "")
					return
				}
			}
		}
	}()
}

func (s *IPCServer) handleReadMessages(w http.ResponseWriter, req IPCRequest) {
	childContainer, err := s.resolveChildContainer(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, IPCResponse{
			Status: "error",
			Error:  err.Error(),
		})
		return
	}

	count := req.Count
	if count <= 0 {
		count = 10
	}
	if count > 50 {
		count = 50
	}

	s.daemon.logInfo("IPC: read_messages request from %s for child %s (count=%d)", req.Parent, childContainer, count)

	writeJSON(w, http.StatusAccepted, IPCResponse{
		Status: "accepted",
	})

	s.daemon.wg.Add(1)
	go func() {
		defer s.daemon.wg.Done()

		messages, err := s.readClaudeMessages(childContainer, count)
		if err != nil {
			errMsg := err.Error()
			s.updateRequestFile(req.Parent, req.ID, IPCRequestStatusFailed, "", errMsg)
			return
		}

		s.updateRequestFileWithMessages(req.Parent, req.ID, messages)
	}()
}

func (s *IPCServer) handleSendMessage(w http.ResponseWriter, req IPCRequest) {
	childContainer, err := s.resolveChildContainer(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, IPCResponse{
			Status: "error",
			Error:  err.Error(),
		})
		return
	}

	if req.Message == "" {
		writeJSON(w, http.StatusBadRequest, IPCResponse{
			Status: "error",
			Error:  "missing required field: message",
		})
		return
	}

	// Cap message size at 50KB
	if len(req.Message) > 50*1024 {
		writeJSON(w, http.StatusBadRequest, IPCResponse{
			Status: "error",
			Error:  "message exceeds 50KB limit",
		})
		return
	}

	s.daemon.logInfo("IPC: send_message request from %s for child %s", req.Parent, childContainer)

	writeJSON(w, http.StatusAccepted, IPCResponse{
		Status: "accepted",
	})

	s.daemon.wg.Add(1)
	go func() {
		defer s.daemon.wg.Done()

		// Verify Claude is running
		if !container.IsClaudeRunning(childContainer) {
			s.updateRequestFile(req.Parent, req.ID, IPCRequestStatusFailed, "", "claude process not running in child container")
			return
		}

		// Write message to temp file in child container
		writeCmd := exec.Command("docker", "exec", "-i", childContainer, "tee", "/tmp/maestro-msg")
		writeCmd.Stdin = strings.NewReader(req.Message)
		writeCmd.Stdout = nil
		if err := writeCmd.Run(); err != nil {
			s.updateRequestFile(req.Parent, req.ID, IPCRequestStatusFailed, "", "failed to write message to child: "+err.Error())
			return
		}

		// Load into tmux buffer
		loadCmd := exec.Command("docker", "exec", childContainer, "tmux", "load-buffer", "/tmp/maestro-msg")
		if err := loadCmd.Run(); err != nil {
			s.updateRequestFile(req.Parent, req.ID, IPCRequestStatusFailed, "", "failed to load tmux buffer: "+err.Error())
			return
		}

		// Paste into Claude pane
		pasteCmd := exec.Command("docker", "exec", childContainer, "tmux", "paste-buffer", "-t", "main:0", "-d")
		if err := pasteCmd.Run(); err != nil {
			s.updateRequestFile(req.Parent, req.ID, IPCRequestStatusFailed, "", "failed to paste message: "+err.Error())
			return
		}

		// Press enter
		enterCmd := exec.Command("docker", "exec", childContainer, "tmux", "send-keys", "-t", "main:0", "C-m")
		if err := enterCmd.Run(); err != nil {
			s.updateRequestFile(req.Parent, req.ID, IPCRequestStatusFailed, "", "failed to send enter key: "+err.Error())
			return
		}

		// Clean up temp file
		cleanCmd := exec.Command("docker", "exec", childContainer, "rm", "-f", "/tmp/maestro-msg")
		_ = cleanCmd.Run()

		// Remove idle flag proactively (prevents race with hooks)
		rmIdleCmd := exec.Command("docker", "exec", childContainer, "rm", "-f", "/home/node/.maestro/claude-idle")
		_ = rmIdleCmd.Run()

		s.updateRequestFile(req.Parent, req.ID, IPCRequestStatusFulfilled, "", "")
	}()
}

// updateRequestFileWithMessages updates a request file with message results
func (s *IPCServer) updateRequestFileWithMessages(containerName, requestID string, messages []IPCMessage) {
	if !validRequestID.MatchString(requestID) {
		s.daemon.logError("IPC: invalid request ID format: %s", requestID)
		return
	}

	requestPath := fmt.Sprintf("/home/node/.maestro/requests/%s.json", requestID)

	readCmd := exec.Command("docker", "exec", containerName, "cat", requestPath)
	output, err := readCmd.Output()
	if err != nil {
		s.daemon.logError("IPC: failed to read request file %s in %s: %v", requestID, containerName, err)
		return
	}

	var reqFile IPCRequestFile
	if err := json.Unmarshal(output, &reqFile); err != nil {
		s.daemon.logError("IPC: failed to parse request file %s: %v", requestID, err)
		return
	}

	reqFile.Status = IPCRequestStatusFulfilled
	now := time.Now()
	reqFile.FulfilledAt = &now
	reqFile.Messages = messages

	updatedJSON, err := json.MarshalIndent(reqFile, "", "  ")
	if err != nil {
		s.daemon.logError("IPC: failed to marshal updated request file: %v", err)
		return
	}

	writeCmd := exec.Command("docker", "exec", "-i", containerName, "tee", requestPath)
	writeCmd.Stdin = strings.NewReader(string(updatedJSON))
	writeCmd.Stdout = nil
	if err := writeCmd.Run(); err != nil {
		s.daemon.logError("IPC: failed to write updated request file %s in %s: %v", requestID, containerName, err)
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
