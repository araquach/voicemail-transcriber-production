package main

import (
	"context"
	"fmt"
	"golang.org/x/oauth2"
	gmailapi "google.golang.org/api/gmail/v1"
	"log"
	"net/http"
	"os"
	"time"

	"voicemail-transcriber-production/internal/auth"
	"voicemail-transcriber-production/internal/config"
	"voicemail-transcriber-production/internal/gmail"
	"voicemail-transcriber-production/internal/logger"

	"google.golang.org/api/option"
)

func main() {
	logger.Init()
	config.LoadEnv()
	auth.SetupOAuthConfig()

	authURL := auth.Config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	logger.Info.Println("🔗 Visit this URL to authenticate:", authURL)

	// Try to seed Firestore with initial history ID if authenticated
	if _, err := os.Stat("token.json"); err == nil {
		token := auth.LoadToken("token.json")
		ctx := context.Background()
		client := auth.Config.Client(ctx, token)

		srv, err := gmailapi.NewService(ctx, option.WithHTTPClient(client))
		if err == nil {
			err = gmail.InitFirestoreHistory(ctx, srv, nil)
			if err != nil {
				logger.Warn.Printf("⚠️ Failed to seed Firestore with history ID: %v", err)
			}
		} else {
			logger.Error.Printf("Failed to create Gmail client: %v", err)
		}
	}

	http.HandleFunc("/callback", auth.HandleCallback)
	http.HandleFunc("/retrieve", gmail.HistoryRetrieveHandler)
	http.HandleFunc("/setup-watch", func(w http.ResponseWriter, r *http.Request) {
		ctx := context.Background()
		token := auth.LoadToken("token.json")
		client := auth.Config.Client(ctx, token)

		srv, err := gmailapi.NewService(ctx, option.WithHTTPClient(client))
		if err != nil {
			http.Error(w, "Failed to create Gmail service", 500)
			return
		}

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

	// Wait for token to be ready before enabling /notify
	go func() {
		for !auth.IsTokenReady {
			fmt.Println("⏳ Waiting for token to become ready before enabling /notify...")
			time.Sleep(1 * time.Second)
		}
		http.HandleFunc("/notify", gmail.PubSubHandler)
		fmt.Println("✅ /notify handler is now active")
	}()

	logger.Info.Println("🚀 Listening on :8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
