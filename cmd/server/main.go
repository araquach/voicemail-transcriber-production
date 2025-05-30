package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"reflect"
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

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		logger.Info.Printf("📥 Incoming request: %s %s", r.Method, r.URL.Path)
		logger.Info.Printf("Headers: %+v", r.Header)
		logger.Info.Printf("Proto: %s", r.Proto)
		logger.Info.Printf("TLS: %v", r.TLS != nil)

		rw := &responseWriter{ResponseWriter: w}
		next.ServeHTTP(rw, r)

		duration := time.Since(start)
		logger.Info.Printf("📤 Response: %d %s (%v)", rw.status, http.StatusText(rw.status), duration)
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

	// Debug endpoint
	mux.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) {
		info := map[string]interface{}{
			"protocol": r.Proto,
			"tls":      r.TLS != nil,
			"headers":  r.Header,
			"remote":   r.RemoteAddr,
			"host":     r.Host,
			"method":   r.Method,
			"path":     r.URL.Path,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	})

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

	// Notify endpoint with enhanced error handling
	mux.HandleFunc("/notify", func(w http.ResponseWriter, r *http.Request) {
		if !state.isReady() {
			http.Error(w, "Service initializing", http.StatusServiceUnavailable)
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

	// Other endpoints...
	mux.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		logger.Info.Printf("📝 Test handler called from: %s", r.RemoteAddr)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
			"time":   time.Now().Format(time.RFC3339),
		})
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Create connection state monitoring channel
	connStateChan := make(chan http.ConnState, 100)

	// Configure server with explicit TLS settings
	srv := &http.Server{
		Addr:    fmt.Sprintf("0.0.0.0:%s", port),
		Handler: loggingMiddleware(mux),
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			NextProtos: []string{"h2", "http/1.1"},
		},
		ReadTimeout:    60 * time.Second,
		WriteTimeout:   60 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20,
		ConnState: func(conn net.Conn, state http.ConnState) {
			select {
			case connStateChan <- state:
				logger.Debug.Printf("Connection from %v changed to %v", conn.RemoteAddr(), state)
			default:
				logger.Warn.Printf("Connection state channel full, dropped state %v for %v", state, conn.RemoteAddr())
			}
		},
	}

	// Monitor connection states
	go func() {
		for state := range connStateChan {
			logger.Info.Printf("🔌 Connection state changed to: %v", state)
		}
	}()

	// Log server configuration
	logger.Info.Printf("🔍 Server Configuration:")
	logger.Info.Printf("- Address: %s", srv.Addr)
	logger.Info.Printf("- Read Timeout: %v", srv.ReadTimeout)
	logger.Info.Printf("- Write Timeout: %v", srv.WriteTimeout)
	logger.Info.Printf("- TLS Min Version: %v", tls.VersionTLS12)
	logger.Info.Printf("- Registered Routes:")
	WalkMuxPaths(mux)

	logger.Info.Printf("🚀 Server starting on %s", srv.Addr)
	logger.Info.Printf("🌐 Build Version: %s", os.Getenv("BUILD_VERSION"))

	if err := srv.ListenAndServe(); err != nil {
		logger.Error.Fatalf("❌ Server failed to start: %v", err)
	}
}

func WalkMuxPaths(mux *http.ServeMux) {
	rv := reflect.ValueOf(mux).Elem()
	rv = rv.FieldByName("m")

	if rv.IsValid() {
		for _, k := range rv.MapKeys() {
			logger.Info.Printf("  - %s", k.String())
		}
	}
}
