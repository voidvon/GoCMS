// Package membermail provides shared, encrypted SMTP configuration and TLS delivery.
package membermail

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Reserved encryption scope, distinct from OAuth providers and legacy site IDs.
const SecretScope int64 = -2

type Config struct {
	SupportEmail string `json:"support_email"`
	Enabled      bool   `json:"enabled"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	TLS          string `json:"tls"`
	Username     string `json:"username"`
	From         string `json:"from_address"`
	Name         string `json:"from_name"`
	PasswordSet  bool   `json:"password_set"`
	Revision     string `json:"revision"`
	Encrypted    string `json:"-"`
}

func Ensure(ctx context.Context, db *sql.DB) error {
	_, e := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS gocms_platform_mail_config(id INTEGER PRIMARY KEY CHECK(id=1),config TEXT NOT NULL,secret_ciphertext TEXT NOT NULL DEFAULT '',revision TEXT NOT NULL)`)
	return e
}
func Load(ctx context.Context, db *sql.DB) (Config, error) {
	c := Config{Port: 587, TLS: "starttls"}
	var raw, enc, revision string
	e := db.QueryRowContext(ctx, `SELECT config,secret_ciphertext,revision FROM gocms_platform_mail_config WHERE id=1`).Scan(&raw, &enc, &revision)
	if errors.Is(e, sql.ErrNoRows) {
		return c, nil
	}
	if e != nil {
		return c, e
	}
	if e = json.Unmarshal([]byte(raw), &c); e != nil {
		return c, e
	}
	c.Encrypted = enc
	c.PasswordSet = enc != ""
	c.Revision = revision
	return c, nil
}
func Address(value string) bool {
	a, e := mail.ParseAddress(value)
	return e == nil && a.Address == value && len(value) <= 254 && !strings.ContainsAny(value, "\r\n")
}
func Validate(c Config) error {
	if c.SupportEmail != "" && !Address(c.SupportEmail) {
		return errors.New("请填写有效的联系邮箱")
	}
	if len(c.Host) > 253 || len(c.Username) > 254 || len(c.Name) > 200 || strings.ContainsAny(c.Host+c.Username+c.Name, "\r\n\x00") {
		return errors.New("邮件配置格式不正确或过长")
	}
	if c.Port < 1 || c.Port > 65535 || (c.TLS != "tls" && c.TLS != "starttls") {
		return errors.New("请选择有效端口及 TLS 或 STARTTLS")
	}
	if c.Host != "" && (strings.ContainsAny(c.Host, " /\\\t:@?#") && net.ParseIP(c.Host) == nil) {
		return errors.New("SMTP 主机只填写域名或 IP，不包含协议和端口")
	}
	if c.From != "" && !Address(c.From) {
		return errors.New("请填写有效的发件邮箱")
	}
	if c.Enabled && (c.Host == "" || !Address(c.From) || (c.Username != "" && c.Encrypted == "")) {
		return errors.New("开启前请填写 SMTP 主机、发件邮箱和认证密码")
	}
	return nil
}
func Message(c Config, to, subject, body string) ([]byte, error) {
	if !Address(to) || !Address(c.From) || strings.ContainsAny(subject+c.Name, "\r\n\x00") {
		return nil, errors.New("无效邮件地址或标题")
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", (&mail.Address{Name: c.Name, Address: c.From}).String(), (&mail.Address{Address: to}).String(), mime.QEncoding.Encode("UTF-8", subject), time.Now().Format(time.RFC1123Z))
	q := quotedprintable.NewWriter(&b)
	if _, e := q.Write([]byte(body)); e != nil {
		return nil, e
	}
	if e := q.Close(); e != nil {
		return nil, e
	}
	return b.Bytes(), nil
}
func Send(ctx context.Context, c Config, password, to, subject, body string) error {
	return sendTLS(ctx, c, password, to, subject, body, &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12})
}
func sendTLS(ctx context.Context, c Config, password, to, subject, body string, tc *tls.Config) error {
	if e := Validate(c); e != nil {
		return e
	}
	data, e := Message(c, to, subject, body)
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	conn, e := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(c.Host, strconv.Itoa(c.Port)))
	if e != nil {
		return e
	}
	defer conn.Close()
	rawConn := conn
	stop := context.AfterFunc(ctx, func() { rawConn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)
	if c.TLS == "tls" {
		secure := tls.Client(conn, tc)
		if e = secure.HandshakeContext(ctx); e != nil {
			return e
		}
		conn = secure
	}
	client, e := smtp.NewClient(conn, c.Host)
	if e != nil {
		return e
	}
	defer client.Close()
	if c.TLS == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP server does not support STARTTLS")
		}
		if e = client.StartTLS(tc); e != nil {
			return e
		}
	}
	if c.Username != "" {
		if e = client.Auth(smtp.PlainAuth("", c.Username, password, c.Host)); e != nil {
			return e
		}
	}
	if e = client.Mail(c.From); e != nil {
		return e
	}
	if e = client.Rcpt(to); e != nil {
		return e
	}
	writer, e := client.Data()
	if e != nil {
		return e
	}
	if _, e = writer.Write(data); e != nil {
		return e
	}
	if e = writer.Close(); e != nil {
		return e
	}
	return client.Quit()
}
