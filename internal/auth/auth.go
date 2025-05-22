package auth

import (
	"context"
	"fmt"
	"os"
	"voicemail-transcriber-production/internal/logger"
	"voicemail-transcriber-production/internal/secret"

	"golang.org/x/oauth2/google"
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

	// Load credentials from Secret Manager
	jsonCredentials, err := secret.LoadSecret(ctx, "gmail-credentials-json")
	if err != nil {
		return nil, fmt.Errorf("error loading credentials from Secret Manager: %w", err)
	}
	logger.Info.Println("✅ Retrieved credentials from Secret Manager")

	// Create credentials configuration with explicit credentials
	creds, err := google.CredentialsFromJSON(ctx, jsonCredentials,
		gmail.GmailSendScope,
		gmail.GmailModifyScope,
		gmail.GmailReadonlyScope,
	)
	if err != nil {
		return nil, fmt.Errorf("error creating credentials: %w", err)
	}

	// Create the Gmail service with explicit credentials
	srv, err := gmail.NewService(ctx, option.WithCredentials(creds))
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
