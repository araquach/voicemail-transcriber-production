package secret

import (
	"context"
	"fmt"
	"os"
	"strings"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	secretpb "google.golang.org/genproto/googleapis/cloud/secretmanager/v1"
)

// LoadSecret fetches the latest version of a secret from Secret Manager
func LoadSecret(ctx context.Context, secretName string) ([]byte, error) {
	// First check if secret is available as environment variable
	envName := strings.ToUpper(strings.ReplaceAll(secretName, "-", "_"))
	if envValue := os.Getenv(envName); envValue != "" {
		return []byte(envValue), nil
	}

	// If not in environment, fall back to Secret Manager
	if secretName == "" {
		return nil, fmt.Errorf("secret name must not be empty")
	}

	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create Secret Manager client: %w", err)
	}
	defer client.Close()

	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		return nil, fmt.Errorf("GCP_PROJECT_ID environment variable is not set")
	}

	accessRequest := &secretpb.AccessSecretVersionRequest{
		Name: fmt.Sprintf("projects/%s/secrets/%s/versions/latest", projectID, secretName),
	}

	result, err := client.AccessSecretVersion(ctx, accessRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to access secret version: %w", err)
	}

	return result.Payload.Data, nil
}
