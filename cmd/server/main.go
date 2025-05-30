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
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
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

func handleNotify(w http.ResponseWriter, r *http.Request, state *AppState, reqID string) {
	logger.Info.Printf("[%s] 📥 Handling notify request: %s %s", reqID, r.Method, r.URL.Path)

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

	if !state.isReady() {
		logger.Warn.Printf("[%s] ⚠️ Service not ready", reqID)
		http.Error(w, "Service initializing", http.StatusServiceUnavailable)
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

	err = gmail.PubSubHandler(w, newReq)
	if err != nil {
		logger.Error.Printf("[%s] ❌ PubSubHandler error: %v", reqID, err)
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

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

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		status := map[string]interface{}{
			"status": "ok",
			"ready":  state.isReady(),
			"time":   time.Now().Format(time.RFC3339),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	})

	mux.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) {
		reqID := uuid.New().String()[:8]
		info := map[string]interface{}{
			"id":           reqID,
			"ready":        state.isReady(),
			"timestamp":    time.Now().Format(time.RFC3339),
			"buildVersion": os.Getenv("BUILD_VERSION"),
			"request": map[string]interface{}{
				"method":     r.Method,
				"uri":        r.RequestURI,
				"proto":      r.Proto,
				"headers":    r.Header,
				"remoteAddr": r.RemoteAddr,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
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
		MaxHeaderBytes: 1 << 20,
	}

	logger.Info.Printf("🚀 Server starting on port %s", port)
	logger.Info.Printf("🌐 Build Version: %s", os.Getenv("BUILD_VERSION"))

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error.Fatalf("❌ Server failed to start: %v", err)
	}
}
