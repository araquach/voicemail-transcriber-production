package main

import (
	"context"
	"fmt"
	gmailapi "google.golang.org/api/gmail/v1"
	"net/http"
	"os"
	"voicemail-transcriber-production/internal/auth"
	"voicemail-transcriber-production/internal/gmail"
	"voicemail-transcriber-production/internal/logger"
)

func main() {
	logger.Init()

	ctx := context.Background()
	srv, err := auth.LoadGmailService(ctx)
	if err != nil {
		logger.Error.Fatalf("Failed to load Gmail service: %v", err)
	}

	// Try to seed Firestore
	if err := gmail.InitFirestoreHistory(ctx, srv, nil); err != nil {
		logger.Warn.Printf("⚠️ Failed to seed Firestore with history ID: %v", err)
	}

	http.HandleFunc("/retrieve", gmail.HistoryRetrieveHandler)
	http.HandleFunc("/setup-watch", func(w http.ResponseWriter, r *http.Request) {
		req := &gmailapi.WatchRequest{
			TopicName: os.Getenv("PUBSUB_TOPIC_NAME"),
			LabelIds:  []string{"INBOX"},
		}

		resp, err := srv.Users.Watch("me", req).Do()
		if err != nil {
			logger.Error.Printf("❌ Failed to set up Gmail watch: %v", err)
			http.Error(w, "Gmail watch setup failed", 500)
			return
		}

		logger.Info.Printf("📌 Gmail watch established. New history ID: %v", resp.HistoryId)
		fmt.Fprintln(w, "✅ Gmail watch successfully re-established!")
	})

	http.HandleFunc("/notify", gmail.PubSubHandler)
	logger.Info.Println("✅ /notify handler registered")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	logger.Info.Printf("🚀 Listening on :%s...", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		logger.Error.Fatalf("❌ Server failed to start: %v", err)
	}
}
