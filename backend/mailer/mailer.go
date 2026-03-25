package mailer

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net/smtp"
	"strconv"
)

// This code as been stolen from tac -> https://github.com/tac4ttack

type ConfMailer struct {
	SmtpServer string   `yaml:"smtp_server"`
	SmtpPort   int      `yaml:"smtp_port"`
	SmtpAuth   bool     `yaml:"smtp_Auth"`
	SmtpUser   string   `yaml:"smtp_user"`
	SmtpPass   string   `yaml:"smtp_pass"`
	SmtpTls    bool     `yaml:"smtp_tls"`
	Helo       string   `yaml:"Helo"`
	FromName   string   `yaml:"from_name"`
	FromMail   string   `yaml:"from_mail"`
	Recipients []string `yaml:"recipients"`
}

type mailer struct {
	SmtpServer string
	SmtpPort   int
	SmtpAuth   bool
	SmtpUser   string
	SmtpPass   string
	SmtpTls    bool
	Helo       string
	FromName   string
	FromMail   string
	Auth       smtp.Auth
	Client     *smtp.Client
	Recipients []string
}

var m mailer

func GetConf() mailer {
	return m
}

func GetRecipients() []string {
	return m.Recipients
}

func InitMailer(conf ConfMailer) {
	m = mailer{
		SmtpPort:   conf.SmtpPort,
		SmtpUser:   conf.SmtpUser,
		SmtpPass:   conf.SmtpPass,
		SmtpServer: conf.SmtpServer,
		SmtpAuth:   conf.SmtpAuth,
		SmtpTls:    conf.SmtpTls,
		Helo:       conf.Helo,
		FromName:   conf.FromName,
		FromMail:   conf.FromMail,
		Recipients: conf.Recipients,
		Auth:       nil,
		Client:     nil,
	}
}

func Send(to []string, subject string, body string, html bool) error {
	var err error

	// Connection to the remote SMTP server
	if m.Client, err = smtp.Dial(m.SmtpServer + ":" + strconv.Itoa(m.SmtpPort)); err != nil {
		return err
	}
	defer m.Client.Close()

	// Sending HELO / EHLO message to the server
	if err = m.Client.Hello(m.Helo); err != nil {
		m.Client.Reset()
		return err
	}

	// Start TLS command if server requires it
	if m.SmtpTls {
		if err = m.Client.StartTLS(
			&tls.Config{
				ServerName:         m.SmtpServer,
				InsecureSkipVerify: true}); err != nil {
			m.Client.Reset()
			return err
		}
	}

	// SMTP Authentication
	if m.SmtpAuth {
		if err = m.Client.Auth(smtp.PlainAuth("", m.SmtpUser, m.SmtpPass, m.SmtpServer)); err != nil {
			m.Client.Reset()
			return err
		}
	}

	// Set the sender
	if err = m.Client.Mail(m.FromMail); err != nil {
		m.Client.Reset()
		return err
	}

	// Set the recipients
	for _, r := range to {
		if err = m.Client.Rcpt(r); err != nil {
			m.Client.Reset()
			return nil
		}
	}

	// Prepare the body
	var wc io.WriteCloser
	if wc, err = m.Client.Data(); err != nil {
		m.Client.Reset()
		return err
	}
	var data string
	if html {

		data = string(
			composeHTML(to,
				m.FromName+"<"+m.FromMail+">",
				subject,
				body))
	} else {

		data = string(
			compose(to,
				m.FromName+"<"+m.FromMail+">",
				subject,
				body))
	}
	if _, err = io.WriteString(wc, data); err != nil {
		m.Client.Reset()
		return err
	}
	if err = wc.Close(); err != nil {
		m.Client.Reset()
		return err
	}

	// Send the QUIT command and close connection
	if err = m.Client.Quit(); err != nil {
		m.Client.Reset()
		return err
	}

	return nil
}

func compose(to []string, from string, subject string, body string) []byte {
	// Formatting MIME headers
	tmp := make(map[string]string)
	tmp["From"] = from
	tmp["To"] = ""
	for i := 0; i < len(to); i++ {
		if to[i] != "" {
			tmp["To"] += to[i]
			if i+1 < len(to) {
				tmp["To"] += ","
			}
		}
	}
	tmp["Subject"] = subject
	tmp["MIME-Version"] = "1.0"
	tmp["Content-Type"] = "text/plain; charset=\"utf-8\""
	tmp["Content-Transfer-Encoding"] = "base64"

	msg := ""
	// Formatting MIME headers into our message
	for i, j := range tmp {
		msg += fmt.Sprintf("%s: %s\r\n", i, j)
	}
	// Adding the email content into our mesage
	msg += "\r\n" + base64.StdEncoding.EncodeToString([]byte(body))
	return []byte(msg)
}

func composeHTML(to []string, from string, subject string, body string) []byte {
	// Formatting MIME headers
	tmp := make(map[string]string)
	tmp["From"] = from
	tmp["To"] = ""
	for i := 0; i < len(to); i++ {
		if to[i] != "" {
			tmp["To"] += to[i]
			if i+1 < len(to) {
				tmp["To"] += ","
			}
		}
	}
	tmp["Subject"] = subject
	tmp["MIME-Version"] = "1.0"
	tmp["Content-Type"] = "text/html; charset=\"UTF-8\";"
	tmp["Content-Transfer-Encoding"] = "base64"

	msg := ""
	// Formatting MIME headers into our message
	for i, j := range tmp {
		msg += fmt.Sprintf("%s: %s\r\n", i, j)
	}
	// Adding the email content into our mesage
	msg += "\r\n" + base64.StdEncoding.EncodeToString([]byte(body))
	return []byte(msg)
}
