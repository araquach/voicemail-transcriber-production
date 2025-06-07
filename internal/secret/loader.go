package secret

import (
	"context"
	"fmt"
	"os"
	"strings"
	"voicemail-transcriber-production/internal/logger"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	secretpb "google.golang.org/genproto/googleapis/cloud/secretmanager/v1"
)

// LoadSecret fetches the latest version of a secret from Secret Manager
// LoadSecret fetches the latest version of a secret from Secret Manager
func LoadSecret(ctx context.Context, secretName string) ([]byte, error) {
	// First check if secret is available as environment variable
	envName := strings.ToUpper(strings.ReplaceAll(secretName, "-", "_"))
	if envValue := os.Getenv(envName); envValue != "" {
		logger.Info.Printf("🔍 Debug: Found secret %s in environment variables", secretName)
		return []byte(envValue), nil
	}
	logger.Info.Printf("🔍 Debug: Secret %s not found in environment, trying Secret Manager", secretName)

	// If not in environment, fall back to Secret Manager
	if secretName == "" {
		return nil, fmt.Errorf("secret name must not be empty")
	}

	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		logger.Error.Printf("❌ Debug: Failed to create Secret Manager client: %v", err)
		return nil, fmt.Errorf("failed to create Secret Manager client: %w", err)
	}
	defer client.Close()

	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		return nil, fmt.Errorf("GCP_PROJECT_ID environment variable is not set")
	}
	logger.Info.Printf("🔍 Debug: Attempting to access secret %s in project %s", secretName, projectID)

	accessRequest := &secretpb.AccessSecretVersionRequest{
		Name: fmt.Sprintf("projects/%s/secrets/%s/versions/latest", projectID, secretName),
	}

	result, err := client.AccessSecretVersion(ctx, accessRequest)
	if err != nil {
		logger.Error.Printf("❌ Debug: Failed to access secret version: %v", err)
		return nil, fmt.Errorf("failed to access secret version: %w", err)
	}

	logger.Info.Printf("✅ Debug: Successfully retrieved secret %s from Secret Manager", secretName)
	return result.Payload.Data, nil
}
