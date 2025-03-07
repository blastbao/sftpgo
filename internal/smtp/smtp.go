// Copyright (C) 2019-2022  Nicola Murino
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

// Package smtp provides supports for sending emails
package smtp

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"path/filepath"
	"time"

	mail "github.com/xhit/go-simple-mail/v2"

	"github.com/drakkan/sftpgo/v2/internal/logger"
	"github.com/drakkan/sftpgo/v2/internal/util"
)

const (
	logSender = "smtp"
)

// EmailContentType defines the support content types for email body
type EmailContentType int

// Supported email body content type
const (
	EmailContentTypeTextPlain EmailContentType = iota
	EmailContentTypeTextHTML
)

const (
	templateEmailDir      = "email"
	templatePasswordReset = "reset-password.html"
)

var (
	smtpServer     *mail.SMTPServer
	from           string
	emailTemplates = make(map[string]*template.Template)
)

// IsEnabled returns true if an SMTP server is configured
func IsEnabled() bool {
	return smtpServer != nil
}

// Config defines the SMTP configuration to use to send emails
type Config struct {
	// Location of SMTP email server. Leavy empty to disable email sending capabilities
	Host string `json:"host" mapstructure:"host"`
	// Port of SMTP email server
	Port int `json:"port" mapstructure:"port"`
	// From address, for example "SFTPGo <sftpgo@example.com>".
	// Many SMTP servers reject emails without a `From` header so, if not set,
	// SFTPGo will try to use the username as fallback, this may or may not be appropriate
	From string `json:"from" mapstructure:"from"`
	// SMTP username
	User string `json:"user" mapstructure:"user"`
	// SMTP password. Leaving both username and password empty the SMTP authentication
	// will be disabled
	Password string `json:"password" mapstructure:"password"`
	// 0 Plain
	// 1 Login
	// 2 CRAM-MD5
	AuthType int `json:"auth_type" mapstructure:"auth_type"`
	// 0 no encryption
	// 1 TLS
	// 2 start TLS
	Encryption int `json:"encryption" mapstructure:"encryption"`
	// Domain to use for HELO command, if empty localhost will be used
	Domain string `json:"domain" mapstructure:"domain"`
	// Path to the email templates. This can be an absolute path or a path relative to the config dir.
	// Templates are searched within a subdirectory named "email" in the specified path
	TemplatesPath string `json:"templates_path" mapstructure:"templates_path"`
}

// Initialize initialized and validates the SMTP configuration
func (c *Config) Initialize(configDir string) error {
	smtpServer = nil
	if c.Host == "" {
		logger.Debug(logSender, "", "configuration disabled, email capabilities will not be available")
		return nil
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("smtp: invalid port %v", c.Port)
	}
	if c.AuthType < 0 || c.AuthType > 2 {
		return fmt.Errorf("smtp: invalid auth type %v", c.AuthType)
	}
	if c.Encryption < 0 || c.Encryption > 2 {
		return fmt.Errorf("smtp: invalid encryption %v", c.Encryption)
	}
	templatesPath := util.FindSharedDataPath(c.TemplatesPath, configDir)
	if templatesPath == "" {
		return fmt.Errorf("smtp: invalid templates path %#v", templatesPath)
	}
	loadTemplates(filepath.Join(templatesPath, templateEmailDir))
	from = c.From
	smtpServer = mail.NewSMTPClient()
	smtpServer.Host = c.Host
	smtpServer.Port = c.Port
	smtpServer.Username = c.User
	smtpServer.Password = c.Password
	smtpServer.Authentication = c.getAuthType()
	smtpServer.Encryption = c.getEncryption()
	smtpServer.KeepAlive = false
	smtpServer.ConnectTimeout = 10 * time.Second
	smtpServer.SendTimeout = 120 * time.Second
	if c.Domain != "" {
		smtpServer.Helo = c.Domain
	}
	logger.Debug(logSender, "", "configuration successfully initialized, host: %#v, port: %v, username: %#v, auth: %v, encryption: %v, helo: %#v",
		smtpServer.Host, smtpServer.Port, smtpServer.Username, smtpServer.Authentication, smtpServer.Encryption, smtpServer.Helo)
	return nil
}

func (c *Config) getEncryption() mail.Encryption {
	switch c.Encryption {
	case 1:
		return mail.EncryptionSSLTLS
	case 2:
		return mail.EncryptionSTARTTLS
	default:
		return mail.EncryptionNone
	}
}

func (c *Config) getAuthType() mail.AuthType {
	if c.User == "" && c.Password == "" {
		return mail.AuthNone
	}
	switch c.AuthType {
	case 1:
		return mail.AuthLogin
	case 2:
		return mail.AuthCRAMMD5
	default:
		return mail.AuthPlain
	}
}

func loadTemplates(templatesPath string) {
	logger.Debug(logSender, "", "loading templates from %#v", templatesPath)

	passwordResetPath := filepath.Join(templatesPath, templatePasswordReset)
	pwdResetTmpl := util.LoadTemplate(nil, passwordResetPath)

	emailTemplates[templatePasswordReset] = pwdResetTmpl
}

// RenderPasswordResetTemplate executes the password reset template
func RenderPasswordResetTemplate(buf *bytes.Buffer, data any) error {
	if smtpServer == nil {
		return errors.New("smtp: not configured")
	}
	return emailTemplates[templatePasswordReset].Execute(buf, data)
}

// SendEmail tries to send an email using the specified parameters.
func SendEmail(to []string, subject, body string, contentType EmailContentType, attachments ...mail.File) error {
	if smtpServer == nil {
		return errors.New("smtp: not configured")
	}
	smtpClient, err := smtpServer.Connect()
	if err != nil {
		return fmt.Errorf("smtp: unable to connect: %w", err)
	}

	email := mail.NewMSG()
	if from != "" {
		email.SetFrom(from)
	} else {
		email.SetFrom(smtpServer.Username)
	}
	email.AddTo(to...).SetSubject(subject)
	switch contentType {
	case EmailContentTypeTextPlain:
		email.SetBody(mail.TextPlain, body)
	case EmailContentTypeTextHTML:
		email.SetBody(mail.TextHTML, body)
	default:
		return fmt.Errorf("smtp: unsupported body content type %v", contentType)
	}
	for _, attachment := range attachments {
		email.Attach(&attachment)
	}
	if email.Error != nil {
		return fmt.Errorf("smtp: email error: %w", email.Error)
	}
	return email.Send(smtpClient)
}
