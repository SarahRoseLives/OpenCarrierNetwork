package fcm

import (
	"context"
	"fmt"
	"log"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/option"
)

type Client struct {
	messaging *messaging.Client
}

func NewClient(serviceAccountPath string) (*Client, error) {
	opt := option.WithCredentialsFile(serviceAccountPath)
	app, err := firebase.NewApp(context.Background(), nil, opt)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize firebase app: %w", err)
	}

	msg, err := app.Messaging(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get messaging client: %w", err)
	}

	log.Printf("FCM client initialized")
	return &Client{messaging: msg}, nil
}

// SendCallNotification sends a high-priority push notification for an incoming call
func (c *Client) SendCallNotification(token, callID, callerNumber, callerName string) error {
	if c == nil || c.messaging == nil {
		return fmt.Errorf("FCM client not initialized")
	}

	message := &messaging.Message{
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

	_, err := c.messaging.Send(context.Background(), message)
	if err != nil {
		return fmt.Errorf("failed to send FCM message: %w", err)
	}

	log.Printf("FCM: sent call notification to %s (call %s from %s)", token[:20], callID, callerNumber)
	return nil
}

// SendVoicemailNotification sends a data message announcing a new voicemail.
func (c *Client) SendVoicemailNotification(token, callerNumber, callerName string) error {
	if c == nil || c.messaging == nil {
		return fmt.Errorf("FCM client not initialized")
	}
	message := &messaging.Message{
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
	_, err := c.messaging.Send(context.Background(), message)
	if err != nil {
		return fmt.Errorf("failed to send FCM voicemail message: %w", err)
	}
	log.Printf("FCM: sent voicemail notification to %s", token[:20])
	return nil
}
