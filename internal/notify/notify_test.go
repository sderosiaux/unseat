package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Slack tests ---

func TestSlackNotifier_Success(t *testing.T) {
	var received slackPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		err := json.NewDecoder(r.Body).Decode(&received)
		require.NoError(t, err)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewSlackNotifier(srv.URL, "#it-ops")
	err := n.Notify(context.Background(), Message{
		Title:    "User removed from Linear",
		Body:     "charlie@co.com was removed during sync",
		Provider: "linear",
		Email:    "charlie@co.com",
		Action:   "removed",
	})
	require.NoError(t, err)

	assert.Equal(t, "#it-ops", received.Channel)
	assert.Contains(t, received.Text, "User removed from Linear")
	assert.Contains(t, received.Text, "charlie@co.com")
	assert.Contains(t, received.Text, "linear")
}

func TestSlackNotifier_WebhookError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := NewSlackNotifier(srv.URL, "#it-ops")
	err := n.Notify(context.Background(), Message{Title: "test", Provider: "x", Email: "a@b.com", Action: "removed"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

// --- Email tests ---

func TestEmailNotifier_Success(t *testing.T) {
	var (
		capturedAddr string
		capturedFrom string
		capturedTo   []string
		capturedMsg  []byte
	)

	e := &EmailNotifier{
		host: "smtp.test.com",
		port: 587,
		from: "noreply@co.com",
		to:   "admin@co.com",
		sendFunc: func(addr string, _ smtp.Auth, from string, to []string, msg []byte) error {
			capturedAddr = addr
			capturedFrom = from
			capturedTo = to
			capturedMsg = msg
			return nil
		},
	}

	err := e.Notify(context.Background(), Message{
		Title:    "User removed from Figma",
		Body:     "old@co.com was removed during sync",
		Provider: "figma",
		Email:    "old@co.com",
		Action:   "removed",
	})
	require.NoError(t, err)

	assert.Equal(t, "smtp.test.com:587", capturedAddr)
	assert.Equal(t, "noreply@co.com", capturedFrom)
	assert.Equal(t, []string{"admin@co.com"}, capturedTo)
	assert.Contains(t, string(capturedMsg), "Subject: User removed from Figma")
	assert.Contains(t, string(capturedMsg), "old@co.com")
}

func TestEmailNotifier_SendError(t *testing.T) {
	e := &EmailNotifier{
		host: "smtp.test.com",
		port: 587,
		from: "noreply@co.com",
		to:   "admin@co.com",
		sendFunc: func(string, smtp.Auth, string, []string, []byte) error {
			return fmt.Errorf("550 mailbox not found")
		},
	}

	err := e.Notify(context.Background(), Message{Title: "test", Provider: "x", Email: "a@b.com", Action: "removed"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "email send to admin@co.com")
}

// --- Dispatcher tests ---

func TestDispatcher_NilSafe(t *testing.T) {
	var d *Dispatcher
	err := d.Notify(context.Background(), Message{Title: "test"})
	assert.NoError(t, err)
}

func TestDispatcher_RoutesSlackAndEmail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var emailSent bool
	cfg := NotifyConfig{
		SlackWebhookURL: srv.URL,
		SMTPHost:        "smtp.test.com",
		SMTPPort:        587,
		SMTPFrom:        "noreply@co.com",
	}

	d := NewDispatcher([]string{"slack:#it-ops", "email:admin@co.com"}, cfg)
	require.Len(t, d.notifiers, 2)

	// Patch the email notifier's sendFunc so we don't actually send
	for _, n := range d.notifiers {
		if en, ok := n.(*EmailNotifier); ok {
			en.sendFunc = func(string, smtp.Auth, string, []string, []byte) error {
				emailSent = true
				return nil
			}
		}
	}

	err := d.Notify(context.Background(), Message{
		Title: "test", Provider: "linear", Email: "a@b.com", Action: "removed",
	})
	require.NoError(t, err)
	assert.True(t, emailSent)
}

func TestDispatcher_SkipsInvalidChannel(t *testing.T) {
	d := NewDispatcher([]string{"invalid-no-colon", "unknown:thing"}, NotifyConfig{})
	assert.Empty(t, d.notifiers)
}

func TestDispatcher_SkipsSlackWithoutWebhook(t *testing.T) {
	d := NewDispatcher([]string{"slack:#ops"}, NotifyConfig{})
	assert.Empty(t, d.notifiers)
}

func TestDispatcher_SkipsEmailWithoutSMTP(t *testing.T) {
	d := NewDispatcher([]string{"email:admin@co.com"}, NotifyConfig{})
	assert.Empty(t, d.notifiers)
}

func TestDispatcher_PartialFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError) // Slack fails
	}))
	defer srv.Close()

	cfg := NotifyConfig{
		SlackWebhookURL: srv.URL,
		SMTPHost:        "smtp.test.com",
		SMTPPort:        587,
		SMTPFrom:        "noreply@co.com",
	}

	d := NewDispatcher([]string{"slack:#ops", "email:admin@co.com"}, cfg)
	// Patch email to succeed
	for _, n := range d.notifiers {
		if en, ok := n.(*EmailNotifier); ok {
			en.sendFunc = func(string, smtp.Auth, string, []string, []byte) error { return nil }
		}
	}

	err := d.Notify(context.Background(), Message{
		Title: "test", Provider: "x", Email: "a@b.com", Action: "removed",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "1/2 notifiers failed")
}
