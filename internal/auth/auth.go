package auth

import (
	"encoding/json"
	"os"

	"voicemail-transcriber-production/internal/logger"
	"voicemail-transcriber-production/internal/secret"

	"golang.org/x/oauth2"
)

var IsTokenReady bool = false

func LoadToken() *oauth2.Token {
	creds := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	logger.Debug.Printf("GOOGLE_APPLICATION_CREDENTIALS = %s", creds)
	logger.Debug.Printf("GCP_PROJECT_ID = %s", os.Getenv("GCP_PROJECT_ID"))
	if creds == "" {
		logger.Error.Fatal("GOOGLE_APPLICATION_CREDENTIALS not set")
	}

	data, err := secret.LoadSecret("gmail-token-json")
	if err != nil {
		logger.Error.Fatalf("Unable to load token from Secret Manager: %v", err)
	}

	var token oauth2.Token
	if err := json.Unmarshal(data, &token); err != nil {
		logger.Error.Fatalf("Unable to parse token JSON: %v", err)
	}
	return &token
}
