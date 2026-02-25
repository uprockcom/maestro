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
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/caddyserver/certmagic"
)

func main() {
	// Required environment variables
	signalAPI := mustEnv("SIGNAL_API")
	botNumber := mustEnv("SIGNAL_NUMBER")
	keysFile := mustEnv("API_KEYS_FILE")

	// Optional environment variables
	tlsDomain := os.Getenv("TLS_DOMAIN")
	tlsEmail := os.Getenv("TLS_EMAIL")
	messageTTL := envDuration("MESSAGE_TTL", 5*time.Minute)

	log.Printf("signal-relay starting")
	log.Printf("  signal-api: %s", signalAPI)
	log.Printf("  bot-number: %s", botNumber)
	log.Printf("  message-ttl: %s", messageTTL)

	// Load API keys
	keyStore, err := LoadKeyStore(keysFile)
	if err != nil {
		log.Fatalf("failed to load API keys: %v", err)
	}
	log.Printf("  loaded %d API key(s)", len(keyStore.users))

	// Create message router
	router := NewMessageRouter(messageTTL)

	// Start background pruning
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go router.StartPruning(ctx)

	// Create server
	srv := NewServer(signalAPI, botNumber, router, keyStore)

	// Start polling signal-cli for incoming messages
	go srv.StartPolling(ctx)
	handler := srv.Handler()

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)

	if tlsDomain != "" {
		// Production: certmagic handles ACME, ports 80+443
		log.Printf("  tls-domain: %s", tlsDomain)
		log.Printf("  tls-email: %s", tlsEmail)

		certmagic.DefaultACME.Agreed = true
		certmagic.DefaultACME.Email = tlsEmail

		go func() {
			<-sigCh
			log.Println("shutting down...")
			cancel()
			os.Exit(0)
		}()

		log.Printf("starting HTTPS server on :443 (domain: %s)", tlsDomain)
		if err := certmagic.HTTPS([]string{tlsDomain}, handler); err != nil {
			log.Fatalf("certmagic HTTPS failed: %v", err)
		}
	} else {
		// Development: plain HTTP on 8080
		httpServer := &http.Server{
			Addr:    ":8080",
			Handler: handler,
		}

		go func() {
			<-sigCh
			log.Println("shutting down...")
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			httpServer.Shutdown(shutdownCtx)
		}()

		log.Printf("starting HTTP server on :8080")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		fmt.Fprintf(os.Stderr, "required environment variable %s is not set\n", key)
		os.Exit(1)
	}
	return strings.TrimSpace(v)
}

func envDuration(key string, defaultVal time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Printf("warning: invalid %s value %q, using default %s", key, v, defaultVal)
		return defaultVal
	}
	return d
}
