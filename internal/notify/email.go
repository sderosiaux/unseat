package notify

import (
	"context"
	"fmt"
	"net/smtp"
)

// SMTPSendFunc abstracts smtp.SendMail for testing.
type SMTPSendFunc func(addr string, a smtp.Auth, from string, to []string, msg []byte) error

// EmailNotifier sends notifications via SMTP.
type EmailNotifier struct {
	host     string
	port     int
	from     string
	to       string
	auth     smtp.Auth
	sendFunc SMTPSendFunc // nil = use smtp.SendMail
}

// NewEmailNotifier creates an email notifier with PLAIN auth.
func NewEmailNotifier(host string, port int, from, to, user, pass string) *EmailNotifier {
	var auth smtp.Auth
	if user != "" {
		auth = smtp.PlainAuth("", user, pass, host)
	}
	return &EmailNotifier{
		host: host,
		port: port,
		from: from,
		to:   to,
		auth: auth,
	}
}

func (e *EmailNotifier) Notify(_ context.Context, msg Message) error {
	subject := msg.Title
	body := fmt.Sprintf("Provider: %s\nUser: %s\nAction: %s\n\n%s",
		msg.Provider, msg.Email, msg.Action, msg.Body)

	raw := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		e.from, e.to, subject, body)

	addr := fmt.Sprintf("%s:%d", e.host, e.port)
	send := e.sendFunc
	if send == nil {
		send = smtp.SendMail
	}
	if err := send(addr, e.auth, e.from, []string{e.to}, []byte(raw)); err != nil {
		return fmt.Errorf("email send to %s: %w", e.to, err)
	}
	return nil
}
