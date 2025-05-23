package auth

import (
	"context"
	"fmt"
	"os"
	"voicemail-transcriber-production/internal/logger"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

var IsTokenReady bool

func LoadGmailService(ctx context.Context) (*gmail.Service, error) {
	userToImpersonate := os.Getenv("EMAIL_RESPONSE_ADDRESS")
	if userToImpersonate == "" {
		return nil, fmt.Errorf("EMAIL_RESPONSE_ADDRESS must be set")
	}
	logger.Info.Printf("🔑 Loading credentials for: %s", userToImpersonate)

	// Create the Gmail service using Application Default Credentials
	srv, err := gmail.NewService(ctx, option.WithScopes(
		gmail.GmailSendScope,
		gmail.GmailModifyScope,
		gmail.GmailReadonlyScope,
	))
	if err != nil {
		return nil, fmt.Errorf("failed to create Gmail service: %w", err)
	}

	// Verify credentials
	_, err = srv.Users.GetProfile("me").Do()
	if err != nil {
		return nil, fmt.Errorf("failed to verify credentials: %w", err)
	}

	IsTokenReady = true
	logger.Info.Println("✅ Gmail service successfully initialized")
	return srv, nil
}
