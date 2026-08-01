package common

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net/smtp"
	"slices"
	"strings"
	"time"
)

func generateMessageID() (string, error) {
	split := strings.Split(SMTPFrom, "@")
	if len(split) < 2 {
		return "", fmt.Errorf("invalid SMTP account")
	}
	domain := strings.Split(SMTPFrom, "@")[1]
	return fmt.Sprintf("<%d.%s@%s>", time.Now().UnixNano(), GetRandomString(12), domain), nil
}

func SendEmail(subject string, receiver string, content string) error {
	if SMTPFrom == "" { // for compatibility
		SMTPFrom = SMTPAccount
	}
	id, err2 := generateMessageID()
	if err2 != nil {
		return err2
	}
	if SMTPServer == "" && SMTPAccount == "" {
		return fmt.Errorf("SMTP 服务器未配置")
	}
	encodedSubject := fmt.Sprintf("=?UTF-8?B?%s?=", base64.StdEncoding.EncodeToString([]byte(subject)))
	mail := []byte(fmt.Sprintf("To: %s\r\n"+
		"From: %s <%s>\r\n"+
		"Subject: %s\r\n"+
		"Date: %s\r\n"+
		"Message-ID: %s\r\n"+ // 添加 Message-ID 头
		"Content-Type: text/html; charset=UTF-8\r\n\r\n%s\r\n",
		receiver, SystemName, SMTPFrom, encodedSubject, time.Now().Format(time.RFC1123Z), id, content))
	auth := smtp.PlainAuth("", SMTPAccount, SMTPToken, SMTPServer)
	addr := fmt.Sprintf("%s:%d", SMTPServer, SMTPPort)
	to := strings.Split(receiver, ";")
	var err error
	if SMTPPort == 465 || SMTPSSLEnabled {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: true,
			ServerName:         SMTPServer,
		}
		conn, err := tls.Dial("tcp", fmt.Sprintf("%s:%d", SMTPServer, SMTPPort), tlsConfig)
		if err != nil {
			return logEmailSendError(receiver, "implicit_tls", fmt.Errorf("SMTP TLS connection failed: %w", err))
		}
		client, err := smtp.NewClient(conn, SMTPServer)
		if err != nil {
			_ = conn.Close()
			return logEmailSendError(receiver, "implicit_tls", fmt.Errorf("SMTP client initialization failed after TLS connection: %w", err))
		}
		defer client.Close()
		if err = client.Auth(auth); err != nil {
			return logEmailSendError(receiver, "implicit_tls", fmt.Errorf("SMTP authentication failed: %w", err))
		}
		if err = client.Mail(SMTPFrom); err != nil {
			return logEmailSendError(receiver, "implicit_tls", fmt.Errorf("SMTP MAIL FROM command failed: %w", err))
		}
		receiverEmails := strings.Split(receiver, ";")
		for _, receiverEmail := range receiverEmails {
			if err = client.Rcpt(receiverEmail); err != nil {
				return logEmailSendError(receiver, "implicit_tls", fmt.Errorf("SMTP RCPT TO command failed for %s: %w", MaskEmail(receiverEmail), err))
			}
		}
		w, err := client.Data()
		if err != nil {
			return logEmailSendError(receiver, "implicit_tls", fmt.Errorf("SMTP DATA command failed: %w", err))
		}
		_, err = w.Write(mail)
		if err != nil {
			_ = w.Close()
			return logEmailSendError(receiver, "implicit_tls", fmt.Errorf("SMTP message body write failed: %w", err))
		}
		err = w.Close()
		if err != nil {
			return logEmailSendError(receiver, "implicit_tls", fmt.Errorf("SMTP message submission failed while closing DATA: %w", err))
		}
	} else if isOutlookServer(SMTPAccount) || slices.Contains(EmailLoginAuthServerList, SMTPServer) {
		auth = LoginAuth(SMTPAccount, SMTPToken)
		err = smtp.SendMail(addr, auth, SMTPFrom, to, mail)
		if err != nil {
			return logEmailSendError(receiver, "starttls_login", fmt.Errorf("SMTP send failed: %w", err))
		}
	} else {
		err = smtp.SendMail(addr, auth, SMTPFrom, to, mail)
		if err != nil {
			return logEmailSendError(receiver, "starttls_plain", fmt.Errorf("SMTP send failed: %w", err))
		}
	}
	return nil
}

func logEmailSendError(receiver string, transport string, err error) error {
	SysError(fmt.Sprintf(
		"failed to send email: receiver=%s, smtp_server=%q, smtp_port=%d, transport=%s, smtp_account=%s, smtp_from=%s, error_type=%T, error=%v",
		maskEmailList(receiver), SMTPServer, SMTPPort, transport, MaskEmail(SMTPAccount), MaskEmail(SMTPFrom), err, err,
	))
	return err
}

func maskEmailList(receivers string) string {
	items := strings.Split(receivers, ";")
	for i, item := range items {
		items[i] = MaskEmail(strings.TrimSpace(item))
	}
	return strings.Join(items, ";")
}
