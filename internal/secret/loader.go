package secret

import (
	"context"
	"fmt"
	"os"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"google.golang.org/api/option"
	secretpb "google.golang.org/genproto/googleapis/cloud/secretmanager/v1"
)

// LoadSecret fetches the latest version of a secret from Secret Manager
func LoadSecret(secretName string) ([]byte, error) {
	ctx := context.Background()

	clientOptions := []option.ClientOption{}
	if creds := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); creds != "" {
		clientOptions = append(clientOptions, option.WithCredentialsFile(creds))
	}

	client, err := secretmanager.NewClient(ctx, clientOptions...)
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

func SaveSecret(secretName string, data []byte) error {
	ctx := context.Background()

	clientOptions := []option.ClientOption{}
	if creds := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); creds != "" {
		clientOptions = append(clientOptions, option.WithCredentialsFile(creds))
	}

	client, err := secretmanager.NewClient(ctx, clientOptions...)
	if err != nil {
		return fmt.Errorf("failed to create Secret Manager client: %w", err)
	}
	defer client.Close()

	projectID := os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		return fmt.Errorf("GCP_PROJECT_ID environment variable is not set")
	}

	// Add new version to existing secret
	_, err = client.AddSecretVersion(ctx, &secretpb.AddSecretVersionRequest{
		Parent: fmt.Sprintf("projects/%s/secrets/%s", projectID, secretName),
		Payload: &secretpb.SecretPayload{
			Data: data,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to add secret version: %w", err)
	}

	return nil
}
