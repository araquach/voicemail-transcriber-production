package main

import (
	"context"
	"encoding/json"
	"fmt"
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

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func loggingMiddleware(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		logger.Info.Printf("📥 Incoming request: %s %s", r.Method, r.URL.Path)
		logger.Info.Printf("Headers: %+v", r.Header)
		logger.Info.Printf("Proto: %s (Major: %d, Minor: %d)",
			r.Proto, r.ProtoMajor, r.ProtoMinor)
		logger.Info.Printf("X-Forwarded-Proto: %s", r.Header.Get("X-Forwarded-Proto"))

		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		handler.ServeHTTP(rw, r)

		duration := time.Since(start)
		logger.Info.Printf("📤 Response: %d %s (%v)",
			rw.status, http.StatusText(rw.status), duration)
	})
}

func main() {
	logger.Init()
	logger.Info.Println("🚀 Starting voicemail transcriber service...")
	logger.PrintEnvSummary()

	state := &AppState{}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Initialize services in a goroutine
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
			"protocol":       r.Proto,
			"protocolMajor":  r.ProtoMajor,
			"protocolMinor":  r.ProtoMinor,
			"headers":        r.Header,
			"remote":         r.RemoteAddr,
			"host":           r.Host,
			"method":         r.Method,
			"path":           r.URL.Path,
			"forwardedProto": r.Header.Get("X-Forwarded-Proto"),
			"serverTime":     time.Now().Format(time.RFC3339),
			"ready":          state.isReady(),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	})

	// Notify endpoint
	mux.HandleFunc("/notify", func(w http.ResponseWriter, r *http.Request) {
		// Handle HTTP/2 connection preface
		if r.Method == "PRI" && r.RequestURI == "*" && r.Proto == "HTTP/2.0" {
			logger.Info.Printf("🔄 Received HTTP/2 connection preface")
			w.WriteHeader(http.StatusOK)
			return
		}

		if !state.isReady() {
			http.Error(w, "Service initializing", http.StatusServiceUnavailable)
			return
		}

		// Check content type for PubSub messages
		contentType := r.Header.Get("Content-Type")
		if !strings.Contains(contentType, "application/json") {
			logger.Warn.Printf("⚠️ Invalid content type: %s", contentType)
			http.Error(w, "Invalid content type", http.StatusBadRequest)
			return
		}

		err := gmail.PubSubHandler(w, r)
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
		}
	})

	// Manual history retrieval endpoint
	mux.HandleFunc("/history", gmail.HistoryRetrieveHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Create server with HTTP/2 support
	srv := &http.Server{
		Addr: fmt.Sprintf("0.0.0.0:%s", port),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Handle HTTP/2 connection preface
			if r.Method == "PRI" && r.RequestURI == "*" && r.Proto == "HTTP/2.0" {
				logger.Info.Printf("🔄 HTTP/2 connection preface received")
				w.WriteHeader(http.StatusOK)
				return
			}

			// Add security headers
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")

			loggingMiddleware(mux).ServeHTTP(w, r)
		}),
		ReadTimeout:    60 * time.Second,
		WriteTimeout:   60 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	logger.Info.Printf("🚀 Server starting on %s", srv.Addr)
	logger.Info.Printf("🌐 Build Version: %s", os.Getenv("BUILD_VERSION"))

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error.Fatalf("❌ Server failed to start: %v", err)
	}
}
