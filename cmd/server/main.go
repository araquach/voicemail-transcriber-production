package main

import (
	"context"
	"fmt"
	gmailapi "google.golang.org/api/gmail/v1"
	"net/http"
	"os"
	"time"
	"voicemail-transcriber-production/internal/auth"
	"voicemail-transcriber-production/internal/gmail"
	"voicemail-transcriber-production/internal/logger"

	"cloud.google.com/go/firestore"
)

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
	ticker := time.NewTicker(24 * time.Hour) // Refresh daily
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
	logger.PrintEnvSummary() // This will print all environment variables

	ctx := context.Background()
	srv, err := auth.LoadGmailService(ctx)
	if err != nil {
		logger.Error.Fatalf("Failed to load Gmail service: %v", err)
	}

	fsClient, err := NewFirestoreClient(ctx)
	if err != nil {
		logger.Error.Fatalf("Failed to initialize Firestore client: %v", err)
	}
	defer fsClient.Close()

	// Initialize Gmail watch on startup
	if err := setupGmailWatch(srv); err != nil {
		logger.Error.Printf("❌ Failed to set up initial Gmail watch: %v", err)
	} else {
		logger.Info.Println("✅ Initial Gmail watch established")
	}

	// Start periodic refresh
	done := make(chan bool)
	defer close(done)
	refreshWatchPeriodically(srv, done)

	msg, err := gmail.GetLatestMessage(srv, "me")
	if err != nil {
		logger.Warn.Printf("⚠️ Failed to fetch latest Gmail message: %v", err)
	} else {
		if err := gmail.SaveHistoryIDToFirestore(ctx, fsClient, msg.HistoryId); err != nil {
			logger.Warn.Printf("⚠️ Failed to overwrite history ID in Firestore: %v", err)
		} else {
			logger.Info.Printf("✅ Latest Gmail history ID (%v) saved to Firestore", msg.HistoryId)
		}
	}

	http.HandleFunc("/retrieve", gmail.HistoryRetrieveHandler)

	// Keep the endpoint for manual re-establishment if needed
	http.HandleFunc("/setup-watch", func(w http.ResponseWriter, r *http.Request) {
		if err := setupGmailWatch(srv); err != nil {
			logger.Error.Printf("❌ %v", err)
			http.Error(w, "Gmail watch setup failed", 500)
			return
		}
		fmt.Fprintln(w, "✅ Gmail watch successfully re-established!")
	})

	http.HandleFunc("/notify", gmail.PubSubHandler)
	logger.Info.Println("✅ /notify handler registered")

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	logger.Info.Printf("🚀 Listening on :%s...", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		logger.Error.Fatalf("❌ Server failed to start: %v", err)
	}
}
