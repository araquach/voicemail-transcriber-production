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

var IsTokenReady bool

func LoadGmailService(ctx context.Context) (*gmail.Service, error) {
	userToImpersonate := os.Getenv("EMAIL_RESPONSE_ADDRESS")
	if userToImpersonate == "" {
		return nil, fmt.Errorf("EMAIL_RESPONSE_ADDRESS must be set")
	}
	logger.Info.Printf("🔑 Setting up service account for impersonation of: %s", userToImpersonate)

	// First, create the JWT config for domain-wide delegation
	scopes := []string{
		gmail.GmailSendScope,
		gmail.GmailModifyScope,
		gmail.GmailReadonlyScope,
	}

	config, err := google.JWTConfigFromJSON([]byte(""), scopes...)
	if err != nil {
		return nil, fmt.Errorf("failed to create JWT config: %w", err)
	}

	// Set up domain-wide delegation
	config.Subject = userToImpersonate

	// Create the credentials with impersonation
	ts := config.TokenSource(ctx)

	// Create the Gmail service with the impersonated credentials
	srv, err := gmail.NewService(ctx,
		option.WithTokenSource(ts),
		option.WithScopes(scopes...),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Gmail service: %w", err)
	}

	// Verify credentials with the impersonated account
	profile, err := srv.Users.GetProfile("me").Do()
	if err != nil {
		return nil, fmt.Errorf("failed to verify credentials for %s: %w", userToImpersonate, err)
	}

	if profile.EmailAddress != userToImpersonate {
		return nil, fmt.Errorf("email mismatch: got %s, expected %s", profile.EmailAddress, userToImpersonate)
	}

	IsTokenReady = true
	logger.Info.Printf("✅ Gmail service successfully initialized for: %s", userToImpersonate)
	return srv, nil
}
