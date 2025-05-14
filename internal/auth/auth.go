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

	creds, err := google.FindDefaultCredentials(ctx, gmail.GmailSendScope, gmail.GmailModifyScope)
	if err != nil {
		return nil, fmt.Errorf("failed to get default credentials: %w", err)
	}

	srv, err := gmail.NewService(ctx, option.WithCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("could not create Gmail service: %w", err)
	}

	IsTokenReady = true
	return srv, nil
}
