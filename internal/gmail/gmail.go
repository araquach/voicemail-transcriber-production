package gmail

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/gmail/v1"
	"voicemail-transcriber-production/internal/logger"
)

func SaveAttachment(srv *gmail.Service, user, msgID string, part *gmail.MessagePart, downloadDir string) (string, error) {
	att, err := srv.Users.Messages.Attachments.Get(user, msgID, part.Body.AttachmentId).Do()
	if err != nil {
		return "", fmt.Errorf("failed to retrieve attachment: %w", err)
	}

	data, err := base64.URLEncoding.DecodeString(att.Data)
	if err != nil {
		return "", fmt.Errorf("failed to decode attachment: %w", err)
	}

	filePath := filepath.Join(downloadDir, part.Filename)
	err = os.WriteFile(filePath, data, 0644)
	if err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	logger.Info.Printf("Attachment saved to: %s", filePath)
	return filePath, nil
}

func MarkAsRead(srv *gmail.Service, user, msgID string) {
	_, err := srv.Users.Messages.Modify(user, msgID, &gmail.ModifyMessageRequest{
		RemoveLabelIds: []string{"UNREAD"},
	}).Do()
	if err != nil {
		logger.Error.Printf("Failed to mark email %s as read: %v", msgID, err)
	} else {
		logger.Info.Printf("Marked email %s as read.", msgID)
	}
}

func GetHeader(headers []*gmail.MessagePartHeader, name string) string {
	for _, h := range headers {
		if h.Name == name {
			return h.Value
		}
	}
	return ""
}

func SaveHistoryIDToFirestore(ctx context.Context, client *firestore.Client, id uint64) error {
	_, err := client.Collection("gmail_state").Doc("history").Set(ctx, map[string]interface{}{
		"historyId": int64(id),
	})
	if err != nil {
		return fmt.Errorf("failed to save history ID to Firestore: %w", err)
	}
	logger.Info.Printf("📌 Saved history ID to Firestore: %d", id)
	return nil
}

func LoadHistoryIDFromFirestore(ctx context.Context, client *firestore.Client) (uint64, error) {
	doc, err := client.Collection("gmail_state").Doc("history").Get(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to load history ID from Firestore: %w", err)
	}

	id, err := doc.DataAt("historyId")
	if err != nil {
		return 0, fmt.Errorf("historyId not found in document: %w", err)
	}

	switch v := id.(type) {
	case int64:
		return uint64(v), nil
	case int:
		return uint64(v), nil
	case float64:
		return uint64(v), nil
	default:
		return 0, fmt.Errorf("unexpected type for historyId: %T", id)
	}
}

func GetLatestMessage(srv *gmail.Service, user string) (*gmail.Message, error) {
	msgs, err := srv.Users.Messages.List(user).MaxResults(1).LabelIds("INBOX").Do()
	if err != nil {
		return nil, fmt.Errorf("failed to list messages: %w", err)
	}
	if len(msgs.Messages) == 0 {
		return nil, fmt.Errorf("no messages found")
	}

	msgID := msgs.Messages[0].Id
	msg, err := srv.Users.Messages.Get(user, msgID).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}
	return msg, nil
}
