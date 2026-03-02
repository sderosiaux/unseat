package notify

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// Message describes a lifecycle event notification.
type Message struct {
	Title    string // e.g. "User removed from Linear"
	Body     string // Details
	Provider string // Which SaaS
	Email    string // User email that was affected
	Action   string // "removed", "added", "pending_removal"
}

// NotifyConfig holds credentials for all notification backends.
type NotifyConfig struct {
	SlackWebhookURL string
	SMTPHost        string
	SMTPPort        int
	SMTPFrom        string
	SMTPUser        string
	SMTPPass        string
}

// Notifier sends notifications about lifecycle events.
type Notifier interface {
	Notify(ctx context.Context, msg Message) error
}

// Dispatcher fans out to multiple notifiers. Nil-safe: a nil Dispatcher is a no-op.
type Dispatcher struct {
	notifiers []Notifier
}

// NewDispatcher parses channel strings and builds the appropriate notifiers.
// Supported formats: "slack:#channel", "email:user@example.com".
func NewDispatcher(channels []string, cfg NotifyConfig) *Dispatcher {
	var notifiers []Notifier
	for _, ch := range channels {
		parts := strings.SplitN(ch, ":", 2)
		if len(parts) != 2 {
			slog.Warn("invalid notify channel format, skipping", "channel", ch)
			continue
		}
		scheme, target := parts[0], parts[1]
		switch scheme {
		case "slack":
			if cfg.SlackWebhookURL == "" {
				slog.Warn("slack channel configured but no webhook URL, skipping", "channel", ch)
				continue
			}
			notifiers = append(notifiers, NewSlackNotifier(cfg.SlackWebhookURL, target))
		case "email":
			if cfg.SMTPHost == "" {
				slog.Warn("email channel configured but no SMTP host, skipping", "channel", ch)
				continue
			}
			notifiers = append(notifiers, NewEmailNotifier(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPFrom, target, cfg.SMTPUser, cfg.SMTPPass))
		default:
			slog.Warn("unknown notify channel scheme, skipping", "scheme", scheme, "channel", ch)
		}
	}
	return &Dispatcher{notifiers: notifiers}
}

// Notify sends msg to all configured notifiers. Errors are logged but do not
// short-circuit: every notifier gets a chance to fire.
func (d *Dispatcher) Notify(ctx context.Context, msg Message) error {
	if d == nil {
		return nil
	}
	var errs []error
	for _, n := range d.notifiers {
		if err := n.Notify(ctx, msg); err != nil {
			slog.Error("notification failed", "error", err)
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("notify: %d/%d notifiers failed", len(errs), len(d.notifiers))
	}
	return nil
}
