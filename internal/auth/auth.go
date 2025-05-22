package auth

import (
	"context"
	"fmt"
	"os"

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
	fmt.Printf("Impersonating user: %s\n", userToImpersonate)

	credentialsFile := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	if credentialsFile == "" {
		return nil, fmt.Errorf("GOOGLE_APPLICATION_CREDENTIALS must be set")
	}
	fmt.Printf("Using credentials file: %s\n", credentialsFile)

	scopes := []string{
		gmail.GmailSendScope,
		gmail.GmailModifyScope,
		gmail.GmailReadonlyScope,
	}

	jsonCredentials, err := os.ReadFile(credentialsFile)
	if err != nil {
		return nil, fmt.Errorf("error reading credentials file: %w", err)
	}
	fmt.Printf("Successfully read credentials file (%d bytes)\n", len(jsonCredentials))

	config, err := google.JWTConfigFromJSON(jsonCredentials, scopes...)
	if err != nil {
		return nil, fmt.Errorf("error parsing credentials: %w", err)
	}
	fmt.Printf("Service account email: %s\n", config.Email)

	config.Subject = userToImpersonate
	fmt.Printf("Set impersonation subject to: %s\n", config.Subject)

	ts := config.TokenSource(ctx)
	fmt.Println("Created token source")

	srv, err := gmail.NewService(ctx, option.WithTokenSource(ts))
	if err != nil {
		return nil, fmt.Errorf("failed to create Gmail service: %w", err)
	}

	// Try a simple API call to verify credentials
	_, err = srv.Users.GetProfile("me").Do()
	if err != nil {
		return nil, fmt.Errorf("failed to verify credentials: %w", err)
	}

	IsTokenReady = true
	fmt.Println("Successfully created and verified Gmail service")
	return srv, nil
}
