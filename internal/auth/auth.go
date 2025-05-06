package auth

import (
	"context"
	"fmt"
	"os"

	"voicemail-transcriber-production/internal/logger"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

var IsTokenReady bool = false

func LoadGmailService(ctx context.Context) (*gmail.Service, error) {
	logger.Debug.Printf("GCP_PROJECT_ID = %s", os.Getenv("GCP_PROJECT_ID"))

	creds, err := google.FindDefaultCredentials(ctx, gmail.GmailSendScope)
	if err != nil {
		logger.Error.Printf("Failed to get default credentials: %v", err)
		return nil, fmt.Errorf("could not get default credentials: %w", err)
	}

	srv, err := gmail.NewService(ctx, option.WithCredentials(creds))
	if err != nil {
		logger.Error.Printf("Failed to create Gmail service: %v", err)
		return nil, fmt.Errorf("could not create Gmail service: %w", err)
	}

	IsTokenReady = true
	return srv, nil
}
