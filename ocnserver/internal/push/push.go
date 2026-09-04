// Package push defines the FCM wake-up abstraction used by the signaling
// server. A server may push locally (its own Firebase creds, *fcm.Client) or
// delegate to the registry's shared Firebase project (*registry.Client). Both
// satisfy Sender structurally.
package push

// Sender wakes an offline device with a data message.
type Sender interface {
	SendCallNotification(token, callID, callerNumber, callerName string) error
	// SendVoicemailNotification notifies a device that a new voicemail is
	// waiting from callerNumber/callerName.
	SendVoicemailNotification(token, callerNumber, callerName string) error
}
