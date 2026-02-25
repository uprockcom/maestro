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

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// Server is the relay HTTP server.
type Server struct {
	signalAPI  string // signal-cli-rest-api base URL
	botNumber  string
	router     *MessageRouter
	keyStore   *KeyStore
	httpClient *http.Client
}

// NewServer creates a new relay server.
func NewServer(signalAPI, botNumber string, router *MessageRouter, keyStore *KeyStore) *Server {
	return &Server{
		signalAPI:  signalAPI,
		botNumber:  botNumber,
		router:     router,
		keyStore:   keyStore,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// Handler returns the HTTP handler with all routes configured.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	auth := AuthMiddleware(s.keyStore)

	// Authenticated endpoints
	mux.Handle("POST /v2/send", auth(http.HandlerFunc(s.handleSend)))
	mux.Handle("GET /v1/receive/{number}", auth(http.HandlerFunc(s.handleReceive)))

	// Internal/public endpoints (no auth)
	mux.HandleFunc("POST /internal/webhook", s.handleWebhook)
	mux.HandleFunc("GET /v1/about", s.handleAbout)
	mux.HandleFunc("GET /health", s.handleHealth)

	return mux
}

// handleSend proxies send requests to signal-cli-rest-api.
func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"failed to read request body"}`, http.StatusBadRequest)
		return
	}

	// Forward to signal-cli
	req, err := http.NewRequestWithContext(r.Context(), "POST", s.signalAPI+"/v2/send", strings.NewReader(string(body)))
	if err != nil {
		http.Error(w, `{"error":"failed to create upstream request"}`, http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		log.Printf("send proxy error: %v", err)
		http.Error(w, `{"error":"upstream request failed"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Relay the response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// handleReceive returns queued messages for the authenticated user.
func (s *Server) handleReceive(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	// Parse cursor
	var afterCursor uint64
	if v := r.URL.Query().Get("after"); v != "" {
		fmt.Sscanf(v, "%d", &afterCursor)
	}

	messages, maxID := s.router.Receive(user.Recipient, afterCursor)
	if messages == nil {
		messages = []ReceivedMessage{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Max-ID", fmt.Sprintf("%d", maxID))
	json.NewEncoder(w).Encode(messages)
}

// handleWebhook receives incoming messages from signal-cli's webhook.
// This endpoint is kept for backward compatibility but the relay now primarily
// uses active polling (see poller.go) since signal-cli json-rpc mode webhooks
// do not reliably deliver text messages.
func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	var msg ReceivedMessage
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		log.Printf("webhook decode error: %v", err)
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	// Only route data messages (ignore typing indicators, receipts, etc.)
	if msg.Envelope.DataMessage == nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	log.Printf("webhook: message from %s routed to queue", msg.Envelope.Source)
	s.router.Route(msg)
	w.WriteHeader(http.StatusOK)
}

// handleAbout proxies the health check to signal-cli.
func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	req, err := http.NewRequestWithContext(r.Context(), "GET", s.signalAPI+"/v1/about", nil)
	if err != nil {
		http.Error(w, `{"error":"failed to create upstream request"}`, http.StatusInternalServerError)
		return
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		log.Printf("about proxy error: %v", err)
		http.Error(w, `{"error":"signal-cli unavailable"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// handleHealth returns the relay's own health status.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}
