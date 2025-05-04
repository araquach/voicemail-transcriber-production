package main

import (
	"context"
	"fmt"
	"golang.org/x/oauth2"
	gmailapi "google.golang.org/api/gmail/v1"
	"net/http"
	"os"
	"voicemail-transcriber-production/internal/auth"
	"voicemail-transcriber-production/internal/config"
	"voicemail-transcriber-production/internal/gmail"
	"voicemail-transcriber-production/internal/logger"

	"google.golang.org/api/option"
)

func main() {
	logger.Init()
	config.LoadEnv()

	// Load token once
	ctx := context.Background()
	token := auth.LoadToken()
	client := oauth2.NewClient(ctx, oauth2.StaticTokenSource(token))

	// Setup Gmail service
	srv, err := gmailapi.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		logger.Error.Fatalf("Failed to create Gmail client: %v", err)
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
