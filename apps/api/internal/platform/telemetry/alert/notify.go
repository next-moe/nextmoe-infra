package alert

import (
	"context"
	"fmt"

	"api/internal/infrastructure/mail"
)

type Notification struct {
	To      string
	Subject string
	Heading string
	HTML    string
}

type Notifier interface {
	Send(ctx context.Context, n Notification) error
}

type EmailNotifier struct {
	mail *mail.Mailer
}

func NewEmailNotifier(m *mail.Mailer) *EmailNotifier {
	return &EmailNotifier{mail: m}
}

func (e *EmailNotifier) Send(ctx context.Context, n Notification) error {
	if e == nil || e.mail == nil {
		return fmt.Errorf("mail not configured")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return e.mail.SendShelled(n.To, n.Subject, n.Heading, n.HTML)
}
