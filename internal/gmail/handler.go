package gmail

import (
	"cloud.google.com/go/firestore"
	"context"
	"fmt"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
	"net/http"
	"net/mail"
	"os"
	"voicemail-transcriber-production/internal/logger"
	"voicemail-transcriber-production/internal/transcriber"
)

type PubSubMessage struct {
	Message struct {
		Data string `json:"data"`
	} `json:"message"`
}

var processedMessages = make(map[string]bool)

func InitFirestoreHistory(ctx context.Context, srv *gmail.Service, fsClient *firestore.Client) error {
	msgList, err := srv.Users.Messages.List("me").MaxResults(1).Do()
	if err != nil {
		return fmt.Errorf("failed to list messages: %w", err)
	}

	if len(msgList.Messages) == 0 {
		return fmt.Errorf("no messages found")
	}

	latestMsgID := msgList.Messages[0].Id
	msg, err := srv.Users.Messages.Get("me", latestMsgID).Format("metadata").Do()
	if err != nil {
		return fmt.Errorf("failed to get message: %w", err)
	}

	historyID := msg.HistoryId
	if historyID == 0 {
		return fmt.Errorf("history ID is missing from message")
	}

	err = SaveHistoryIDToFirestore(ctx, fsClient, historyID)
	if err != nil {
		return fmt.Errorf("failed to save to Firestore: %w", err)
	}

	logger.Info.Printf("📌 Seeded Firestore with latest Gmail history ID: %d", historyID)
	return nil
}

func PubSubHandler(w http.ResponseWriter, r *http.Request) error {
	logger.Info.Println("📬 PubSubHandler dummy activated")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"dummy-ok"}`))
	return nil
}

//func PubSubHandler(w http.ResponseWriter, r *http.Request) error {
//	start := time.Now()
//	logger.Info.Printf("📨 Received PubSub request from: %s", r.RemoteAddr)
//
//	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
//	defer cancel()
//
//	if !auth.IsTokenReady {
//		logger.Warn.Println("⚠️ Skipping Pub/Sub handling — token not ready")
//		return fmt.Errorf("app not ready: token not available yet")
//	}
//
//	if r.Method != http.MethodPost {
//		return fmt.Errorf("invalid method: %s", r.Method)
//	}
//
//	body, err := io.ReadAll(r.Body)
//	if err != nil {
//		logger.Error.Printf("❌ Failed to read body: %v", err)
//		return fmt.Errorf("failed to read request body: %w", err)
//	}
//
//	logger.Debug.Printf("🐛 Raw body: %s", string(body))
//
//	var msg PubSubMessage
//	if err = json.Unmarshal(body, &msg); err != nil {
//		logger.Error.Printf("❌ Failed to unmarshal PubSub message: %v", err)
//		return fmt.Errorf("invalid JSON: %w", err)
//	}
//
//	decodedData, err := base64.StdEncoding.DecodeString(msg.Message.Data)
//	if err != nil {
//		logger.Error.Printf("❌ Failed to decode message data: %v", err)
//		return fmt.Errorf("invalid base64 data: %w", err)
//	}
//
//	logger.Debug.Printf("📨 Decoded Pub/Sub data: %s", decodedData)
//
//	var notificationData struct {
//		EmailAddress string `json:"emailAddress"`
//		HistoryId    uint64 `json:"historyId"`
//	}
//	if err = json.Unmarshal(decodedData, &notificationData); err != nil {
//		logger.Error.Printf("❌ Failed to unmarshal decoded data: %v", err)
//		return fmt.Errorf("invalid message format: %w", err)
//	}
//
//	fsClient, err := firestore.NewClient(ctx, os.Getenv("GCP_PROJECT_ID"))
//	if err != nil {
//		logger.Error.Printf("❌ Failed to create Firestore client: %v", err)
//		return fmt.Errorf("failed to create Firestore client: %w", err)
//	}
//	defer func() {
//		if err := fsClient.Close(); err != nil {
//			logger.Error.Printf("❌ Error closing Firestore client: %v", err)
//		}
//	}()
//
//	srv, err := auth.LoadGmailService(ctx)
//	if err != nil {
//		logger.Error.Printf("❌ Unable to create Gmail service: %v", err)
//		return fmt.Errorf("failed to create Gmail service: %w", err)
//	}
//
//	logger.Info.Printf("📩 Processing Pub/Sub notification for: %s (History ID: %d)",
//		notificationData.EmailAddress, notificationData.HistoryId)
//
//	if err := ctx.Err(); err != nil {
//		return fmt.Errorf("context error before history processing: %w", err)
//	}
//
//	previousHistoryID, err := LoadHistoryIDFromFirestore(ctx, fsClient)
//	if err != nil {
//		logger.Error.Printf("❌ Could not load history ID from Firestore: %v", err)
//		return fmt.Errorf("failed to load history ID: %w", err)
//	}
//
//	historyCtx, historyCancel := context.WithTimeout(ctx, 30*time.Second)
//	defer historyCancel()
//
//	if err := retrieveHistory(historyCtx, srv, previousHistoryID, fsClient); err != nil {
//		if err == context.DeadlineExceeded {
//			logger.Error.Printf("❌ History retrieval timed out after 30 seconds")
//			return fmt.Errorf("history retrieval timeout: %w", err)
//		}
//		logger.Error.Printf("❌ Failed to retrieve history: %v", err)
//		return fmt.Errorf("failed to retrieve history: %w", err)
//	}
//
//	elapsed := time.Since(start)
//	logger.Info.Printf("✅ PubSub request processed successfully in %v", elapsed)
//
//	if elapsed > 40*time.Second {
//		logger.Warn.Printf("⚠️ Request processing took longer than expected: %v", elapsed)
//	}
//
//	// ✅ Write success HTTP response
//	w.Header().Set("Content-Type", "application/json")
//	w.WriteHeader(http.StatusOK)
//	_ = json.NewEncoder(w).Encode(map[string]string{
//		"status": "ok",
//	})
//
//	return nil
//}

func HistoryRetrieveHandler(w http.ResponseWriter, r *http.Request) {
	logger.Info.Println("🔍 Manual history polling started")

	ctx := context.Background()
	client, err := google.DefaultClient(ctx, gmail.GmailReadonlyScope, gmail.GmailModifyScope)
	if err != nil {
		logger.Error.Printf("❌ Failed to get default client: %v", err)
		http.Error(w, "Unable to get default client", http.StatusInternalServerError)
		return
	}

	srv, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		http.Error(w, "Failed to create Gmail service", http.StatusInternalServerError)
		return
	}

	fsClient, err := firestore.NewClient(ctx, os.Getenv("GCP_PROJECT_ID"))
	if err != nil {
		logger.Error.Fatalf("❌ Failed to create Firestore client: %v", err)
	}
	defer fsClient.Close()

	startHistoryID, err := LoadHistoryIDFromFirestore(ctx, fsClient)
	if err != nil {
		logger.Error.Fatalf("❌ Could not load history ID from Firestore: %v", err)
	}

	retrieveHistory(ctx, srv, startHistoryID, fsClient)

	fmt.Fprintln(w, "✅ History polling complete. Check logs for details.")
}

func retrieveHistory(ctx context.Context, srv *gmail.Service, startHistoryID uint64, fsClient *firestore.Client) error {
	req := srv.Users.History.List("me").
		StartHistoryId(startHistoryID).
		HistoryTypes("messageAdded")

	err := req.Pages(ctx, func(resp *gmail.ListHistoryResponse) error {
		if resp.History == nil {
			logger.Info.Println("No new history records found.")
			return nil
		}

		logger.Info.Printf("🔍 Retrieved %d history records", len(resp.History))

		for _, h := range resp.History {
			for _, m := range h.MessagesAdded {
				if m.Message != nil {
					msgID := m.Message.Id
					logger.Info.Printf("📨 Found message: ID=%s", msgID)

					if processedMessages[msgID] {
						logger.Debug.Printf("⚠️ Skipping already processed message: %s", msgID)
						continue
					}
					processedMessages[msgID] = true

					msg, err := srv.Users.Messages.Get("me", msgID).Format("full").Do()
					if err != nil {
						logger.Error.Printf("Failed to retrieve message %s: %v", msgID, err)
						continue
					}

					from := GetHeader(msg.Payload.Headers, "From")
					logger.Debug.Printf("✉️ From: %s", from)

					parsed, err := mail.ParseAddress(from)
					if err != nil {
						logger.Error.Printf("Failed to parse From header: %v", err)
						continue
					}

					if parsed.Address != "araquach@yahoo.co.uk" {
						logger.Debug.Printf("⏭️ Skipping message from %s", parsed.Address)
						continue
					}

					for _, part := range msg.Payload.Parts {
						if part.Filename != "" && part.Body.AttachmentId != "" {
							filePath, err := SaveAttachment(srv, "me", msg.Id, part, "/tmp")
							if err != nil {
								logger.Error.Printf("Failed to save attachment: %v", err)
								continue
							}

							subject := GetHeader(msg.Payload.Headers, "Subject")
							err = transcriber.TranscribeAndRespond(ctx, filePath, srv, subject)
							if err != nil {
								logger.Error.Printf("Failed to transcribe and respond: %v", err)
							}

							os.Remove(filePath)
							MarkAsRead(srv, "me", msg.Id)
						}
					}
				}
			}
		}

		if resp.HistoryId != 0 {
			if err := SaveHistoryIDToFirestore(ctx, fsClient, resp.HistoryId); err != nil {
				return fmt.Errorf("failed to save updated history ID to Firestore: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("history retrieval error: %w", err)
	}

	return nil
}
