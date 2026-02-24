package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

// Handle registers a typed handler for the given endpoint on the mux.
// The handler receives a decoded request and returns a response + error.
// For GET endpoints, the request is decoded from query parameters.
// For POST/PUT endpoints, the request is decoded from the JSON body.
func Handle[Req, Resp any](mux *http.ServeMux, ep Endpoint[Req, Resp], handler func(r *http.Request, req Req) (Resp, error)) {
	handleEndpoint(mux, ep, nil, handler)
}

// HandleWithAuth registers a typed handler with an auth check that runs BEFORE
// body decoding. This prevents unauthenticated clients from streaming large
// request bodies. The authFn should return a non-nil *Error to reject the request.
func HandleWithAuth[Req, Resp any](mux *http.ServeMux, ep Endpoint[Req, Resp], authFn func(r *http.Request) *Error, handler func(r *http.Request, req Req) (Resp, error)) {
	handleEndpoint(mux, ep, authFn, handler)
}

func handleEndpoint[Req, Resp any](mux *http.ServeMux, ep Endpoint[Req, Resp], authFn func(r *http.Request) *Error, handler func(r *http.Request, req Req) (Resp, error)) {
	mux.HandleFunc(ep.Pattern, func(w http.ResponseWriter, r *http.Request) {
		// Auth check runs before any body decoding
		if authFn != nil {
			if err := authFn(r); err != nil {
				writeError(w, err.Status, err.Message)
				return
			}
		}

		var req Req

		if r.Method == http.MethodGet {
			// Decode from query parameters via JSON round-trip
			if err := decodeQueryParams(r, &req); err != nil {
				writeError(w, http.StatusBadRequest, "invalid query parameters: "+err.Error())
				return
			}
		} else if r.Body != nil && r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				// Treat EOF as "no body" (e.g. chunked transfer with empty payload)
				if err != io.EOF {
					writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
					return
				}
			}
		}

		resp, err := handler(r, req)
		if err != nil {
			if apiErr, ok := err.(*Error); ok {
				writeError(w, apiErr.Status, apiErr.Message)
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(resp)
	})
}

// decodeQueryParams converts URL query parameters into a struct via JSON round-trip.
// Returns an error if the parameters cannot be decoded into the destination struct.
func decodeQueryParams(r *http.Request, dest any) error {
	q := r.URL.Query()
	if len(q) == 0 {
		return nil
	}
	m := make(map[string]any)
	for k, v := range q {
		if len(v) > 0 {
			switch v[0] {
			case "true":
				m[k] = true
			case "false":
				m[k] = false
			default:
				val := v[0]
				// Try integer first to avoid float64 precision loss on large IDs/cursors
				if n, err := strconv.ParseInt(val, 10, 64); err == nil {
					m[k] = n
				} else if u, err := strconv.ParseUint(val, 10, 64); err == nil {
					m[k] = u
				} else if f, err := strconv.ParseFloat(val, 64); err == nil {
					m[k] = f
				} else {
					m[k] = val
				}
			}
		}
	}
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encoding query params: %w", err)
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("decoding query params: %w", err)
	}
	return nil
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{Error: message})
}
