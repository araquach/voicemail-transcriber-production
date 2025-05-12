package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/oauth2"
	"os"

	"voicemail-transcriber-production/internal/logger"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	secretmanagerpb "google.golang.org/genproto/googleapis/cloud/secretmanager/v1"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

var IsTokenReady bool = false

func LoadGmailService(ctx context.Context) (*gmail.Service, error) {
	logger.Debug.Printf("GCP_PROJECT_ID = %s", os.Getenv("GCP_PROJECT_ID"))

	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		return nil, fmt.Errorf("GCP_PROJECT_ID environment variable not set")
	}

	// Create the Secret Manager client
	smClient, err := secretmanager.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create Secret Manager client: %w", err)
	}
	defer smClient.Close()

	// Build the resource name of the secret version
	secretName := fmt.Sprintf("projects/%s/secrets/gmail-credentials-json/versions/latest", projectID)

	// Access the secret version
	accessRequest := &secretmanagerpb.AccessSecretVersionRequest{
		Name: secretName,
	}

	result, err := smClient.AccessSecretVersion(ctx, accessRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to access secret version: %w", err)
	}

	// Parse the secret payload
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(result.Payload.Data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse secret JSON: %w", err)
	}

	webJSON, ok := raw["web"]
	if !ok {
		return nil, fmt.Errorf(`missing "web" field in credentials JSON`)
	}

	config, err := google.ConfigFromJSON(webJSON, gmail.GmailReadonlyScope)
	if err != nil {
		return nil, fmt.Errorf("failed to create config from JSON: %w", err)
	}

	tokSecretName := fmt.Sprintf("projects/%s/secrets/gmail-token-json/versions/latest", projectID)
	tokAccessRequest := &secretmanagerpb.AccessSecretVersionRequest{
		Name: tokSecretName,
	}

	tokResult, err := smClient.AccessSecretVersion(ctx, tokAccessRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to access Gmail token secret version: %w", err)
	}

	var tok oauth2.Token
	if err := json.Unmarshal(tokResult.Payload.Data, &tok); err != nil {
		return nil, fmt.Errorf("failed to unmarshal Gmail token JSON: %w", err)
	}

	client := config.Client(ctx, &tok)
	srv, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("could not create Gmail service: %w", err)
	}

	IsTokenReady = true
	return srv, nil
}
