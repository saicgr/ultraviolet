// Package notifications — SMTP transport.
//
// Minimal stdlib net/smtp wrapper. Honors SMTP_HOST/SMTP_PORT/SMTP_USER/
// SMTP_PASSWORD/SMTP_FROM env vars. Returns a real error (never silently
// degrades) when SMTP_HOST is unset — matches Ultraviolet "no mock data" rule.
package notifications

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strings"
)

// SMTPSender is a thin envelope around net/smtp.SendMail using env-driven config.
// Built once per process; Send is goroutine-safe (stateless).
type SMTPSender struct {
	Host     string
	Port     string
	Username string
	Password string
	From     string
}

// NewSMTPSenderFromEnv returns a sender configured from SMTP_* env vars.
// Returns nil, error when SMTP_HOST is unset so callers can decide whether
// email delivery is required for the deployment.
func NewSMTPSenderFromEnv() (*SMTPSender, error) {
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		return nil, errors.New("smtp: SMTP_HOST unset")
	}
	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = "587"
	}
	from := os.Getenv("SMTP_FROM")
	if from == "" {
		from = "no-reply@" + host
	}
	return &SMTPSender{
		Host:     host,
		Port:     port,
		Username: os.Getenv("SMTP_USER"),
		Password: os.Getenv("SMTP_PASSWORD"),
		From:     from,
	}, nil
}

// Kind reports the channel name (matches delivery_subscription.channel).
func (s *SMTPSender) Kind() string { return "email" }

// Send writes an RFC 822 message to `to` over SMTP-AUTH PLAIN if creds are set,
// otherwise unauthenticated submission. ctx cancellation aborts the dial.
func (s *SMTPSender) Send(ctx context.Context, to, subject, body string) error {
	if s == nil || s.Host == "" {
		return errors.New("smtp: not configured (SMTP_HOST unset)")
	}
	if to == "" {
		return errors.New("smtp: empty recipient")
	}
	addr := net.JoinHostPort(s.Host, s.Port)
	msg := []byte(fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		s.From, to, subject, body,
	))
	var auth smtp.Auth
	if s.Username != "" {
		auth = smtp.PlainAuth("", s.Username, s.Password, s.Host)
	}
	// net/smtp has no ctx hook; honor cancellation by checking before dial.
	if err := ctx.Err(); err != nil {
		return err
	}
	recipients := []string{strings.TrimSpace(to)}
	if err := smtp.SendMail(addr, auth, s.From, recipients, msg); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return nil
}
