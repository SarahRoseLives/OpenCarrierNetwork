package push

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/option"
)

// Client sends push notifications using the network's shared Firebase project.
type Client struct {
	messaging *messaging.Client
}

func NewClient(serviceAccountPath string) (*Client, error) {
	opt := option.WithCredentialsFile(serviceAccountPath)

	// Pull the project id out of the service account so we never depend on
	// ambient environment configuration.
	raw, err := os.ReadFile(serviceAccountPath)
	if err != nil {
		return nil, fmt.Errorf("read service account: %w", err)
	}
	var meta struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, fmt.Errorf("parse service account: %w", err)
	}
	cfg := &firebase.Config{ProjectID: meta.ProjectID}

	app, err := firebase.NewApp(context.Background(), cfg, opt)
	if err != nil {
		return nil, fmt.Errorf("init firebase app: %w", err)
	}
	msg, err := app.Messaging(context.Background())
	if err != nil {
		return nil, fmt.Errorf("init messaging client: %w", err)
	}
	log.Printf("Registry push client initialized (project %s)", meta.ProjectID)
	return &Client{messaging: msg}, nil
}

// SendCallNotification sends the same data message the OCN phone app already
// understands for an incoming call.
func (c *Client) SendCallNotification(token, callID, callerNumber, callerName string) error {
	if c == nil || c.messaging == nil {
		return fmt.Errorf("push client not initialized")
	}
	msg := &messaging.Message{
		Token: token,
		Android: &messaging.AndroidConfig{
			Priority: "high",
		},
		Data: map[string]string{
			"type":          "incoming_call",
			"call_id":       callID,
			"caller_number": callerNumber,
			"caller_name":   callerName,
		},
	}
	_, err := c.messaging.Send(context.Background(), msg)
	if err != nil {
		return fmt.Errorf("send fcm message: %w", err)
	}
	log.Printf("Registry push sent to token %s...", truncate(token, 16))
	return nil
}

// SendVoicemailNotification sends the "new voicemail" data message the OCN
// phone app understands.
func (c *Client) SendVoicemailNotification(token, callerNumber, callerName string) error {
	if c == nil || c.messaging == nil {
		return fmt.Errorf("push client not initialized")
	}
	msg := &messaging.Message{
		Token: token,
		Android: &messaging.AndroidConfig{
			Priority: "high",
		},
		Data: map[string]string{
			"type":          "voicemail",
			"caller_number": callerNumber,
			"caller_name":   callerName,
		},
	}
	_, err := c.messaging.Send(context.Background(), msg)
	if err != nil {
		return fmt.Errorf("send fcm voicemail message: %w", err)
	}
	log.Printf("Registry voicemail push sent to token %s...", truncate(token, 16))
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
