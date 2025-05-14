package transcriber

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/deepgram/deepgram-go-sdk/pkg/api/prerecorded/v1"
	interfaces "github.com/deepgram/deepgram-go-sdk/pkg/client/interfaces"
	client "github.com/deepgram/deepgram-go-sdk/pkg/client/prerecorded"
	"google.golang.org/api/gmail/v1"
	"os"
	"regexp"
	"voicemail-transcriber-production/internal/logger"
	"voicemail-transcriber-production/internal/secret"
)

func TranscribeAndRespond(ctx context.Context, filePath string, srv *gmail.Service, subjectLine string) error {
	options := interfaces.PreRecordedTranscriptionOptions{
		Model:       "nova-2",
		Language:    "en-US",
		SmartFormat: true,
	}
	apiKey, err := secret.LoadSecret(ctx, "deepgram-api-key")
	if err != nil {
		logger.Error.Printf("❌ Failed to load Deepgram API key: %v", err)
		return err
	}
	c := client.New(string(apiKey), interfaces.ClientOptions{Host: "https://api.deepgram.com"})
	dg := prerecorded.New(c)

	res, err := dg.FromFile(ctx, filePath, options)
	if err != nil {
		return fmt.Errorf("transcription failed: %w", err)
	}

	data, _ := json.Marshal(res)
	var resMap map[string]interface{}
	json.Unmarshal(data, &resMap)
	transcript := extractTranscript(resMap)

	logger.Info.Printf("Transcript: %s", transcript)
	phone := extractPhoneNumber(subjectLine)
	return sendEmailResponse(ctx, srv, os.Getenv("EMAIL_RESPONSE_ADDRESS"), "Your Voicemail Transcription", fmt.Sprintf("Caller: %s\n\n%s", phone, transcript))
}

func extractTranscript(resMap map[string]interface{}) string {
	if results, exist := resMap["results"]; exist {
		resultObj := results.(map[string]interface{})
		if channels, exist := resultObj["channels"]; exist {
			channelSlice := channels.([]interface{})
			if len(channelSlice) > 0 {
				if alternatives, exist := channelSlice[0].(map[string]interface{})["alternatives"]; exist {
					alternativeSlice := alternatives.([]interface{})
					if len(alternativeSlice) > 0 {
						if transcript, ok := alternativeSlice[0].(map[string]interface{})["transcript"].(string); ok {
							return transcript
						}
					}
				}
			}
		}
	}
	return "No transcript available."
}

func sendEmailResponse(ctx context.Context, srv *gmail.Service, recipient, subject, body string) error {
	emailBody := fmt.Sprintf("To: %s\r\nSubject: %s\r\n\r\n%s", recipient, subject, body)
	var message gmail.Message
	message.Raw = base64.StdEncoding.EncodeToString([]byte(emailBody))

	_, err := srv.Users.Messages.Send("me", &message).Do()
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	logger.Info.Println("Transcription email sent.")
	return nil
}

func extractPhoneNumber(subject string) string {
	re := regexp.MustCompile(`\b07\d{9}\b`)
	match := re.FindString(subject)
	if match != "" {
		return match
	}
	return "Unknown"
}
