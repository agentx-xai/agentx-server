package notification

import (
	"agentx/server/internal/entity"
	"context"
	"crypto/tls"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

type SMTPConfig struct {
	Address    string
	Username   string
	Password   string
	From       string
	TLSMode    string
	ConsoleURL string
	Timeout    time.Duration
}

type SMTPInvitationSender struct {
	config SMTPConfig
	now    func() time.Time
}

func NewSMTPInvitationSender(config SMTPConfig) (*SMTPInvitationSender, error) {
	host, _, err := net.SplitHostPort(config.Address)
	if err != nil || host == "" {
		return nil, fmt.Errorf("invalid SMTP address")
	}
	if config.TLSMode != "tls" && config.TLSMode != "starttls" && config.TLSMode != "plain" {
		return nil, fmt.Errorf("invalid SMTP TLS mode")
	}
	from, err := mail.ParseAddress(config.From)
	if err != nil || from.Name != "" || from.Address != config.From {
		return nil, fmt.Errorf("invalid SMTP sender")
	}
	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}
	return &SMTPInvitationSender{config: config, now: time.Now}, nil
}

func (s *SMTPInvitationSender) SendInvitation(ctx context.Context, invitation entity.WorkspaceInvitation) error {
	recipient, err := mail.ParseAddress(invitation.Email)
	if err != nil || recipient.Name != "" || recipient.Address != invitation.Email {
		return fmt.Errorf("invitation has an invalid recipient")
	}
	host, _, _ := net.SplitHostPort(s.config.Address)
	dialer := net.Dialer{Timeout: s.config.Timeout}
	connection, err := dialer.DialContext(ctx, "tcp", s.config.Address)
	if err != nil {
		return fmt.Errorf("connect to SMTP server: %w", err)
	}
	defer connection.Close()
	deadline := s.now().Add(s.config.Timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err = connection.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set SMTP deadline: %w", err)
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}
	if s.config.TLSMode == "tls" {
		tlsConnection := tls.Client(connection, tlsConfig)
		if err = tlsConnection.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("establish SMTP TLS: %w", err)
		}
		connection = tlsConnection
	}

	client, err := smtp.NewClient(connection, host)
	if err != nil {
		return fmt.Errorf("initialize SMTP client: %w", err)
	}
	defer client.Close()
	if s.config.TLSMode == "starttls" {
		if err = client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("establish SMTP STARTTLS: %w", err)
		}
	}
	if s.config.Username != "" {
		if err = client.Auth(smtp.PlainAuth("", s.config.Username, s.config.Password, host)); err != nil {
			return fmt.Errorf("authenticate to SMTP server: %w", err)
		}
	}
	if err = client.Mail(s.config.From); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	if err = client.Rcpt(invitation.Email); err != nil {
		return fmt.Errorf("set SMTP recipient: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("start SMTP message: %w", err)
	}
	message := renderInvitationMessage(s.config.From, s.config.ConsoleURL, invitation, s.now().UTC())
	if _, err = w.Write(message); err != nil {
		_ = w.Close()
		return fmt.Errorf("write SMTP message: %w", err)
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("finish SMTP message: %w", err)
	}
	if err = client.Quit(); err != nil {
		return fmt.Errorf("finish SMTP session: %w", err)
	}
	return nil
}

func renderInvitationMessage(from, consoleURL string, invitation entity.WorkspaceInvitation, sentAt time.Time) []byte {
	workspace := printableLine(invitation.WorkspaceName)
	if workspace == "" {
		workspace = printableLine(invitation.WorkspaceSlug)
	}
	if workspace == "" {
		workspace = "an AgentX workspace"
	}
	body := strings.Join([]string{
		"You have been invited to join " + workspace + " with the " + printableLine(string(invitation.Role)) + " role.",
		"",
		"Sign in with this email address, open the Workspace view, and accept the pending invitation:",
		consoleURL,
		"",
		"This invitation expires at " + invitation.ExpiresAt.UTC().Format(time.RFC3339) + ".",
		"If you did not expect this invitation, you can ignore this message.",
		"",
	}, "\r\n")
	headers := []string{
		"From: " + from,
		"To: " + invitation.Email,
		"Subject: " + mime.QEncoding.Encode("UTF-8", "AgentX workspace invitation"),
		"Date: " + sentAt.Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
	}
	return []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + body)
}

func printableLine(value string) string {
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 256 {
		value = value[:256]
	}
	return value
}
