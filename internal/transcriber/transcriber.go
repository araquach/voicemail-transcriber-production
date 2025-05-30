package transcriber

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"voicemail-transcriber-production/internal/logger"
	"voicemail-transcriber-production/internal/secret"

	"google.golang.org/api/gmail/v1"
)

type DeepgramResponse struct {
	Results struct {
		Channels []struct {
			Alternatives []struct {
				Transcript string `json:"transcript"`
			} `json:"alternatives"`
		} `json:"channels"`
	} `json:"results"`
}

func TranscribeAndRespond(ctx context.Context, audioPath string, gmailSrv *gmail.Service, subject string) error {
	// Get API key from Secret Manager
	apiKey, err := secret.LoadSecret(ctx, "deepgram-api-key")
	if err != nil {
		return fmt.Errorf("failed to load Deepgram API key: %w", err)
	}

	// Read audio file
	audioData, err := os.ReadFile(audioPath)
	if err != nil {
		return fmt.Errorf("failed to read audio file: %w", err)
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Create request
	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		"https://api.deepgram.com/v1/listen?language=en-US&model=nova-2&smart_format=true",
		bytes.NewReader(audioData),
	)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Authorization", fmt.Sprintf("Token %s", strings.TrimSpace(string(apiKey))))
	req.Header.Set("Content-Type", "audio/wav")

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("transcription request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("transcription failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var dgResp DeepgramResponse
	if err := json.Unmarshal(body, &dgResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Extract transcript
	if len(dgResp.Results.Channels) == 0 ||
		len(dgResp.Results.Channels[0].Alternatives) == 0 {
		return fmt.Errorf("no transcription results found")
	}

	transcript := dgResp.Results.Channels[0].Alternatives[0].Transcript
	if transcript == "" {
		return fmt.Errorf("empty transcript received")
	}

	logger.Info.Printf("🎯 Transcription successful: %s", transcript)

	// Create email message
	var message gmail.Message
	emailBody := fmt.Sprintf("Transcription of voicemail from: %s\n\n%s", subject, transcript)

	// RFC 2822 email formatting
	emailTo := os.Getenv("EMAIL_RESPONSE_ADDRESS")
	if emailTo == "" {
		return fmt.Errorf("EMAIL_RESPONSE_ADDRESS not set")
	}

	var msg bytes.Buffer
	msg.WriteString(fmt.Sprintf("To: %s\r\n", emailTo))
	msg.WriteString(fmt.Sprintf("Subject: Voicemail Transcription: %s\r\n", subject))
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(emailBody)

	// Encode the message
	message.Raw = base64.URLEncoding.EncodeToString(msg.Bytes())

	// Send the email
	_, err = gmailSrv.Users.Messages.Send("me", &message).Do()
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	logger.Info.Printf("✉️ Transcription email sent successfully")
	return nil
}
