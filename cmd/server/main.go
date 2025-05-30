package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"voicemail-transcriber-production/internal/auth"
	"voicemail-transcriber-production/internal/gmail"
	"voicemail-transcriber-production/internal/logger"

	"cloud.google.com/go/firestore"
	gmailapi "google.golang.org/api/gmail/v1"
)

type AppState struct {
	srv       *gmailapi.Service
	fsClient  *firestore.Client
	ready     bool
	readyLock sync.RWMutex
}

func (s *AppState) setReady(ready bool) {
	s.readyLock.Lock()
	defer s.readyLock.Unlock()
	s.ready = ready
}

func (s *AppState) isReady() bool {
	s.readyLock.RLock()
	defer s.readyLock.RUnlock()
	return s.ready
}

func NewFirestoreClient(ctx context.Context) (*firestore.Client, error) {
	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		return nil, fmt.Errorf("GCP_PROJECT_ID environment variable not set")
	}
	return firestore.NewClient(ctx, projectID)
}

func handlePubSubRequest(w http.ResponseWriter, r *http.Request, state *AppState) {
	if !state.isReady() {
		http.Error(w, "Service initializing", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error.Printf("❌ Failed to read request body: %v", err)
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if len(body) == 0 {
		logger.Warn.Printf("⚠️ Empty request body received")
		http.Error(w, "Empty request body", http.StatusBadRequest)
		return
	}

	logger.Info.Printf("📨 Processing PubSub request with body length: %d bytes", len(body))
	err = gmail.PubSubHandler(w, r)
	if err != nil {
		logger.Error.Printf("❌ PubSubHandler error: %v", err)
		switch {
		case strings.Contains(err.Error(), "not ready"):
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
		case strings.Contains(err.Error(), "invalid"):
			http.Error(w, err.Error(), http.StatusBadRequest)
		default:
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	logger.Info.Printf("✅ Successfully processed PubSub request")
}

func main() {
	logger.Init()
	logger.Info.Println("🚀 Starting voicemail transcriber service...")

	state := &AppState{}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Initialize services
	go func() {
		var err error
		state.srv, err = auth.LoadGmailService(ctx)
		if err != nil {
			logger.Error.Printf("Failed to load Gmail service: %v", err)
			return
		}

		state.fsClient, err = NewFirestoreClient(ctx)
		if err != nil {
			logger.Error.Printf("Failed to initialize Firestore client: %v", err)
			return
		}

		if err := gmail.InitFirestoreHistory(ctx, state.srv, state.fsClient); err != nil {
			logger.Error.Printf("❌ Failed to initialize Firestore history: %v", err)
		} else {
			logger.Info.Println("✅ Firestore history initialized")
		}

		state.setReady(true)
		logger.Info.Println("✅ Application initialization complete")
	}()

	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if state.isReady() {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "OK")
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, "Initializing")
		}
	})

	// Debug endpoint
	mux.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) {
		info := map[string]interface{}{
			"ready":        state.isReady(),
			"serverTime":   time.Now().Format(time.RFC3339),
			"buildVersion": os.Getenv("BUILD_VERSION"),
			"request": map[string]interface{}{
				"method":        r.Method,
				"uri":           r.RequestURI,
				"proto":         r.Proto,
				"contentLength": r.ContentLength,
				"contentType":   r.Header.Get("Content-Type"),
				"remoteAddr":    r.RemoteAddr,
				"headers":       r.Header,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	})

	// Main notify endpoint
	mux.HandleFunc("/notify", func(w http.ResponseWriter, r *http.Request) {
		// Log request details
		logger.Info.Printf("📥 Request: %s %s (Proto: %s)", r.Method, r.URL.Path, r.Proto)

		// Handle HTTP/2 connection preface
		if r.Method == "PRI" && r.RequestURI == "*" && r.Proto == "HTTP/2.0" {
			logger.Info.Printf("🔄 HTTP/2 connection preface received")
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(http.StatusOK)
			return
		}

		// Enforce POST method
		if r.Method != http.MethodPost {
			logger.Warn.Printf("⚠️ Invalid method: %s", r.Method)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Verify content type
		contentType := r.Header.Get("Content-Type")
		if !strings.Contains(contentType, "application/json") {
			logger.Warn.Printf("⚠️ Invalid content type: %s", contentType)
			http.Error(w, "Invalid content type", http.StatusBadRequest)
			return
		}

		handlePubSubRequest(w, r, state)
	})

	// Manual history retrieval endpoint
	mux.HandleFunc("/history", gmail.HistoryRetrieveHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr: fmt.Sprintf("0.0.0.0:%s", port),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Set security headers
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")

			// Log request start
			start := time.Now()
			logger.Info.Printf("👉 Request started: %s %s", r.Method, r.URL.Path)

			// Handle request
			mux.ServeHTTP(w, r)

			// Log request completion
			duration := time.Since(start)
			logger.Info.Printf("👈 Request completed in %v", duration)
		}),
		ReadTimeout:    60 * time.Second,
		WriteTimeout:   60 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	logger.Info.Printf("🚀 Server starting on port %s", port)
	logger.Info.Printf("🌐 Build Version: %s", os.Getenv("BUILD_VERSION"))

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error.Fatalf("❌ Server failed to start: %v", err)
	}
}
