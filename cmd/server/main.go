
package main

import (
	"bytes"
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
	"github.com/google/uuid"
	gmailapi "google.golang.org/api/gmail/v1"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type AppState struct {
	srv       *gmailapi.Service
	fsClient  *firestore.Client
	ready     bool
	readyLock sync.RWMutex
	initOnce  sync.Once
}

func (s *AppState) initialize(ctx context.Context) error {
	var initErr error
	s.initOnce.Do(func() {
		s.srv, initErr = auth.LoadGmailService(ctx)
		if initErr != nil {
			logger.Error.Printf("Failed to load Gmail service: %v", initErr)
			return
		}

		s.fsClient, initErr = firestore.NewClient(ctx, os.Getenv("GCP_PROJECT_ID"))
		if initErr != nil {
			logger.Error.Printf("Failed to initialize Firestore client: %v", initErr)
			return
		}

		if initErr = gmail.InitFirestoreHistory(ctx, s.srv, s.fsClient); initErr != nil {
			logger.Error.Printf("❌ Failed to initialize Firestore history: %v", initErr)
			return
		}

		s.setReady(true)
		logger.Info.Println("✅ Application initialization complete")
	})
	return initErr
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

func handleNotify(w http.ResponseWriter, r *http.Request, state *AppState, reqID string) {
	logger.Info.Printf("[%s] 📥 Processing request: %s %s", reqID, r.Method, r.URL.Path)

	if r.Method != http.MethodPost {
		logger.Warn.Printf("[%s] ⚠️ Invalid method: %s", reqID, r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	contentType := r.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		logger.Warn.Printf("[%s] ⚠️ Invalid content type: %s", reqID, contentType)
		http.Error(w, "Invalid content type", http.StatusBadRequest)
		return
	}

	// Ensure service is initialized
	if err := state.initialize(r.Context()); err != nil {
		logger.Error.Printf("[%s] ❌ Service initialization failed: %v", reqID, err)
		http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
		return
	}

	maxBodySize := int64(1 << 20) // 1 MB
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error.Printf("[%s] ❌ Failed to read body: %v", reqID, err)
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if len(body) == 0 {
		logger.Warn.Printf("[%s] ⚠️ Empty request body", reqID)
		http.Error(w, "Empty request body", http.StatusBadRequest)
		return
	}

	logger.Info.Printf("[%s] 📦 Processing request body: %d bytes", reqID, len(body))

	newReq := r.Clone(r.Context())
	newReq.Body = io.NopCloser(bytes.NewReader(body))

	if err := gmail.PubSubHandler(w, newReq); err != nil {
		logger.Error.Printf("[%s] ❌ Handler error: %v", reqID, err)
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

	logger.Info.Printf("[%s] ✅ Request processed successfully", reqID)
}

func main() {
	logger.Init()
	logger.Info.Println("🚀 Starting voicemail transcriber service...")

	state := &AppState{}

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": state.isReady() ? "ok" : "initializing",
			"time":   time.Now().Format(time.RFC3339),
		})
	})

	mux.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) {
		reqID := uuid.New().String()[:8]
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":            reqID,
			"ready":         state.isReady(),
			"timestamp":     time.Now().Format(time.RFC3339),
			"buildVersion": os.Getenv("BUILD_VERSION"),
			"request": map[string]interface{}{
				"method":      r.Method,
				"uri":        r.RequestURI,
				"proto":      r.Proto,
				"headers":    r.Header,
				"remoteAddr": r.RemoteAddr,
			},
		})
	})

	mux.HandleFunc("/notify", func(w http.ResponseWriter, r *http.Request) {
		reqID := uuid.New().String()[:8]
		handleNotify(w, r, state, reqID)
	})

	mux.HandleFunc("/history", gmail.HistoryRetrieveHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	h2s := &http2.Server{
		IdleTimeout: 120 * time.Second,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := uuid.New().String()[:8]
		start := time.Now()

		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Request-ID", reqID)

		logger.Info.Printf("[%s] 👉 Request started: %s %s %s", reqID, r.Method, r.URL.Path, r.Proto)
		mux.ServeHTTP(w, r)
		logger.Info.Printf("[%s] 👈 Request completed in %v", reqID, time.Since(start))
	})

	server := &http.Server{
		Addr:           fmt.Sprintf("0.0.0.0:%s", port),
		Handler:        h2c.NewHandler(handler, h2s),
		ReadTimeout:    60 * time.Second,
		WriteTimeout:   60 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	logger.Info.Printf("🚀 Server starting on port %s", port)
	logger.Info.Printf("🌐 Build Version: %s", os.Getenv("BUILD_VERSION"))

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error.Fatalf("❌ Server failed to start: %v", err)
	}
}