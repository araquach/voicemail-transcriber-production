package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"voicemail-transcriber-production/internal/logger"
	"voicemail-transcriber-production/internal/secret"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
)

var Config *oauth2.Config

var IsTokenReady bool = false

func SetupOAuthConfig() {
	data, err := secret.LoadSecret("gmail-credentials-json")
	if err != nil {
		logger.Error.Fatalf("Unable to read credentials file: %v", err)
	}

	Config, err = google.ConfigFromJSON(data, gmail.GmailModifyScope, gmail.GmailSendScope)
	if err != nil {
		logger.Error.Fatalf("Unable to parse credentials file: %v", err)
	}
}

func HandleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Missing code", http.StatusBadRequest)
		return
	}

	token, err := Config.Exchange(context.Background(), code)
	if err != nil {
		http.Error(w, "Token exchange failed", http.StatusInternalServerError)
		return
	}

	SaveTokenToSecretManager(token)

	IsTokenReady = true

	fmt.Fprintln(w, "Authentication successful! Token saved.")
	logger.Info.Println("Token saved successfully.")
}

func SaveTokenToSecretManager(token *oauth2.Token) {
	data, err := json.Marshal(token)
	if err != nil {
		logger.Error.Fatalf("Unable to marshal token: %v", err)
	}

	logger.Debug.Printf("GOOGLE_APPLICATION_CREDENTIALS = %s", os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"))
	err = secret.SaveSecret("gmail-token-json", data)
	if err != nil {
		logger.Error.Fatalf("Unable to save token to Secret Manager: %v", err)
	}
	logger.Info.Println("✅ Token saved to Secret Manager.")
}

func LoadToken(_ string) *oauth2.Token {
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
