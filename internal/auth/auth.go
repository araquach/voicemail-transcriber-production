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
	logger.Info.Printf("🔍 Debug: Starting Gmail service initialization for: %s", userToImpersonate)

	// Load service account credentials
	credBytes, err := secret.LoadSecret(ctx, "gmail-token-json")
	if err != nil {
		logger.Error.Printf("❌ Debug: Failed to load service account credentials: %v", err)
		return nil, fmt.Errorf("failed to load service account credentials: %w", err)
	}
	logger.Info.Printf("✅ Debug: Successfully loaded service account credentials")

	scopes := []string{
		gmail.GmailSendScope,
		gmail.GmailModifyScope,
		gmail.GmailReadonlyScope,
	}

	// Create JWT config from service account
	config, err := google.JWTConfigFromJSON(credBytes, scopes...)
	if err != nil {
		logger.Error.Printf("❌ Debug: Failed to create JWT config: %v", err)
		return nil, fmt.Errorf("failed to create JWT config: %w", err)
	}
	logger.Info.Printf("✅ Debug: Successfully created JWT config")

	// Set up domain-wide delegation
	config.Subject = userToImpersonate
	logger.Info.Printf("🔍 Debug: Set impersonation subject to: %s", userToImpersonate)

	// Create token source
	ts := config.TokenSource(ctx)
	logger.Info.Printf("✅ Debug: Created token source")

	// Test token generation
	token, err := ts.Token()
	if err != nil {
		logger.Error.Printf("❌ Debug: Failed to generate token: %v", err)
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}
	logger.Info.Printf("✅ Debug: Successfully generated token (expires: %v)", token.Expiry)

	// Create Gmail service
	srv, err := gmail.NewService(ctx,
		option.WithTokenSource(ts),
		option.WithScopes(scopes...),
	)
	if err != nil {
		logger.Error.Printf("❌ Debug: Failed to create Gmail service: %v", err)
		return nil, fmt.Errorf("failed to create Gmail service: %w", err)
	}
	logger.Info.Printf("✅ Debug: Successfully created Gmail service")

	// Verify credentials
	profile, err := srv.Users.GetProfile("me").Do()
	if err != nil {
		logger.Error.Printf("❌ Debug: Failed to verify credentials: %v", err)
		return nil, fmt.Errorf("failed to verify credentials: %w", err)
	}

	if profile.EmailAddress != userToImpersonate {
		logger.Error.Printf("❌ Debug: Email mismatch - got: %s, expected: %s",
			profile.EmailAddress, userToImpersonate)
		return nil, fmt.Errorf("email mismatch: got %s, expected %s",
			profile.EmailAddress, userToImpersonate)
	}

	IsTokenReady = true
	logger.Info.Printf("✅ Debug: Gmail service fully initialized for: %s", userToImpersonate)
	return srv, nil
}
