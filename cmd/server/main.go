package main

import (
	"bytes"
	"context"
	"fmt"
	gmailapi "google.golang.org/api/gmail/v1"
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

func setupGmailWatch(srv *gmailapi.Service) error {
	req := &gmailapi.WatchRequest{
		TopicName: os.Getenv("PUBSUB_TOPIC_NAME"),
		LabelIds:  []string{"INBOX"},
	}

	resp, err := srv.Users.Watch("me", req).Do()
	if err != nil {
		return fmt.Errorf("failed to set up Gmail watch: %v", err)
	}

	logger.Info.Printf("📌 Gmail watch established. New history ID: %v", resp.HistoryId)
	return nil
}

func refreshWatchPeriodically(srv *gmailapi.Service, done chan bool) {
	ticker := time.NewTicker(24 * time.Hour)
	go func() {
		for {
			select {
			case <-ticker.C:
				if err := setupGmailWatch(srv); err != nil {
					logger.Error.Printf("❌ Failed to refresh Gmail watch: %v", err)
				} else {
					logger.Info.Println("✅ Gmail watch refreshed")
				}
			case <-done:
				ticker.Stop()
				return
			}
		}
	}()
}

func main() {
	logger.Init()
	logger.Info.Println("🚀 Starting voicemail transcriber service...")
	logger.PrintEnvSummary()

	state := &AppState{}

	// Setup context with timeout for initialization
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

		if err := setupGmailWatch(state.srv); err != nil {
			logger.Error.Printf("❌ Failed to set up initial Gmail watch: %v", err)
		} else {
			logger.Info.Println("✅ Initial Gmail watch established")
		}

		// Start periodic refresh
		done := make(chan bool)
		refreshWatchPeriodically(state.srv, done)

		// Initialize history ID
		msg, err := gmail.GetLatestMessage(state.srv, "me")
		if err != nil {
			logger.Warn.Printf("⚠️ Failed to fetch latest Gmail message: %v", err)
		} else {
			if err := gmail.SaveHistoryIDToFirestore(ctx, state.fsClient, msg.HistoryId); err != nil {
				logger.Warn.Printf("⚠️ Failed to overwrite history ID in Firestore: %v", err)
			} else {
				logger.Info.Printf("✅ Latest Gmail history ID (%v) saved to Firestore", msg.HistoryId)
			}
		}

		// Mark the application as ready
		state.setReady(true)
		logger.Info.Println("✅ Application initialization complete")
	}()

	// Health check endpoint
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if state.isReady() {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "OK")
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, "Initializing")
		}
	})

	// Setup other endpoints
	http.HandleFunc("/retrieve", func(w http.ResponseWriter, r *http.Request) {
		if !state.isReady() {
			http.Error(w, "Service initializing", http.StatusServiceUnavailable)
			return
		}
		gmail.HistoryRetrieveHandler(w, r)
	})

	http.HandleFunc("/setup-watch", func(w http.ResponseWriter, r *http.Request) {
		if !state.isReady() {
			http.Error(w, "Service initializing", http.StatusServiceUnavailable)
			return
		}
		if err := setupGmailWatch(state.srv); err != nil {
			logger.Error.Printf("❌ %v", err)
			http.Error(w, "Gmail watch setup failed", 500)
			return
		}
		fmt.Fprintln(w, "✅ Gmail watch successfully re-established!")
	})

	//http.HandleFunc("/notify", func(w http.ResponseWriter, r *http.Request) {
	//	defer func() {
	//		if rec := recover(); rec != nil {
	//			logger.Error.Printf("🔥 Panic recovered in /notify: %v", rec)
	//			http.Error(w, "Internal server error", http.StatusInternalServerError)
	//		}
	//	}()
	//
	//	if r.Method != http.MethodPost {
	//		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	//		return
	//	}
	//
	//	logger.Info.Printf("📬 /notify invoked from: %s", r.RemoteAddr)
	//
	//	// 🕵️ Log the raw request body
	//	body, _ := io.ReadAll(r.Body)
	//	logger.Info.Printf("📨 Raw /notify body: %s", string(body))
	//
	//	// 🔁 Reuse body for PubSubHandler
	//	r.Body = io.NopCloser(bytes.NewReader(body))
	//
	//	logger.Info.Println("🔍 About to call gmail.PubSubHandler")
	//
	//	err := gmail.PubSubHandler(w, r)
	//	if err != nil {
	//		logger.Error.Printf("❌ PubSubHandler error: %v", err)
	//
	//		switch {
	//		case err.Error() == "app not ready: token not available yet":
	//			http.Error(w, err.Error(), http.StatusServiceUnavailable)
	//		case strings.Contains(err.Error(), "invalid"):
	//			http.Error(w, err.Error(), http.StatusBadRequest)
	//		case strings.Contains(err.Error(), "timeout"):
	//			http.Error(w, "Request timeout", http.StatusGatewayTimeout)
	//		default:
	//			http.Error(w, "Internal server error", http.StatusInternalServerError)
	//		}
	//		return
	//	}
	//
	//	logger.Info.Println("📬 PubSubHandler returned without error — success response already sent")
	//})

	http.HandleFunc("/notify", func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error.Printf("🔥 Panic recovered in /notify: %v", rec)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
		}()

		logger.Info.Println("🔥 Entered /notify handler")

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			logger.Error.Printf("❌ Failed reading body: %v", err)
		} else {
			logger.Info.Printf("📨 Raw /notify body: %s", string(body))
		}

		r.Body = io.NopCloser(bytes.NewReader(body))

		err = gmail.PubSubHandler(w, r)
		if err != nil {
			logger.Error.Printf("❌ PubSubHandler error: %v", err)

			switch {
			case err.Error() == "app not ready: token not available yet":
				http.Error(w, err.Error(), http.StatusServiceUnavailable)
			case strings.Contains(err.Error(), "invalid"):
				http.Error(w, err.Error(), http.StatusBadRequest)
			case strings.Contains(err.Error(), "timeout"):
				http.Error(w, "Request timeout", http.StatusGatewayTimeout)
			default:
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
			return
		}

		logger.Info.Println("📬 PubSubHandler returned without error — success response already sent")
	})

	srv := &http.Server{
		Addr:           "0.0.0.0:" + port,
		Handler:        nil,
		ReadTimeout:    60 * time.Second,  // Increased from 15
		WriteTimeout:   60 * time.Second,  // Increased from 15
		IdleTimeout:    120 * time.Second, // Added idle timeout
		MaxHeaderBytes: 1 << 20,
	}

	logger.Info.Printf("🚀 Listening on 0.0.0.0:%s...", port)
	logger.Info.Println("🧭 Running build version:", os.Getenv("BUILD_VERSION"))
	if err := srv.ListenAndServe(); err != nil {
		logger.Error.Fatalf("❌ Server failed to start: %v", err)
	}
}
