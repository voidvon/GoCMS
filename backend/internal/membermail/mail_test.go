package membermail

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSecurityMailMIMEAndValidation(t *testing.T) {
	c := Config{Port: 587, TLS: "starttls", Host: "smtp.example.com", From: "sender@example.com", Name: "账户支持"}
	subject, body := ResetMessage("zh-CN", "https://a.example/shop/reset-password#token=opaque")
	raw, e := Message(c, "person@example.com", subject, body)
	if e != nil {
		t.Fatal(e)
	}
	m, e := mail.ReadMessage(strings.NewReader(string(raw)))
	if e != nil {
		t.Fatal(e)
	}
	decoded, e := (&mime.WordDecoder{}).DecodeHeader(m.Header.Get("Subject"))
	if e != nil || decoded != subject {
		t.Fatal("bad subject", e)
	}
	payload, e := io.ReadAll(quotedprintable.NewReader(m.Body))
	if e != nil || strings.ReplaceAll(string(payload), "\r\n", "\n") != body {
		t.Fatal("bad body", e)
	}
	for _, address := range []string{"victim@example.com\r\nBcc:other@example.com", "name <victim@example.com>", "bad"} {
		if _, e := Message(c, address, subject, body); e == nil {
			t.Fatal("bad address accepted")
		}
	}
	c.Name = "name\r\nBcc:other@example.com"
	if _, e := Message(c, "person@example.com", subject, body); e == nil {
		t.Fatal("header injection accepted")
	}
	for _, mode := range []string{"none", "plaintext", ""} {
		c.TLS = mode
		if Validate(c) == nil {
			t.Fatal("insecure mode accepted")
		}
	}
	for _, lang := range []string{"en", "zh-CN", "es", "fr", "pt", "ru", "ar", "id", "vi", "tr", "th"} {
		s, b := ResetMessage(lang, "https://example.com")
		if s == "" || !strings.Contains(b, "https://example.com") {
			t.Fatal("missing translation")
		}
	}
}
func TestSMTPRefusesPlaintextServer(t *testing.T) {
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	done := make(chan string, 1)
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			done <- ""
			return
		}
		defer conn.Close()
		conn.Write([]byte("220 localhost ESMTP\r\n"))
		scanner := bufio.NewScanner(conn)
		var commands []string
		for scanner.Scan() {
			line := scanner.Text()
			commands = append(commands, line)
			if strings.HasPrefix(line, "EHLO") {
				conn.Write([]byte("250-localhost\r\n250 AUTH PLAIN\r\n"))
			} else {
				conn.Write([]byte("250 OK\r\n"))
			}
		}
		done <- strings.Join(commands, "\n")
	}()
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	n, _ := strconv.Atoi(port)
	c := Config{Host: host, Port: n, TLS: "starttls", Username: "secret-user", From: "sender@example.com"}
	if e = Send(context.Background(), c, "secret-password", "person@example.com", "Test", "Test"); e == nil {
		t.Fatal("plaintext SMTP accepted")
	}
	if commands := <-done; strings.Contains(commands, "AUTH") || strings.Contains(commands, "MAIL") {
		t.Fatal("credentials or mail sent before TLS", commands)
	}
}

func TestSMTPTLSDelivery(t *testing.T) {
	certServer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	cert := certServer.TLS.Certificates[0]
	certServer.Close()
	leaf, e := x509.ParseCertificate(cert.Certificate[0])
	if e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	for _, mode := range []string{"tls", "starttls"} {
		t.Run(mode, func(t *testing.T) {
			listener, e := net.Listen("tcp", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			defer listener.Close()
			done := make(chan error, 1)
			go func() {
				conn, e := listener.Accept()
				if e != nil {
					done <- e
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				secure := mode == "tls"
				config := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
				if secure {
					conn = tls.Server(conn, config)
				}
				reader := bufio.NewReader(conn)
				fmt.Fprint(conn, "220 localhost ESMTP\r\n")
				authenticated, delivered := false, false
				for {
					line, e := reader.ReadString('\n')
					if e != nil {
						done <- e
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"):
						if secure {
							fmt.Fprint(conn, "250-localhost\r\n250 AUTH PLAIN\r\n")
						} else {
							fmt.Fprint(conn, "250-localhost\r\n250 STARTTLS\r\n")
						}
					case strings.HasPrefix(line, "STARTTLS"):
						fmt.Fprint(conn, "220 Ready for TLS\r\n")
						conn = tls.Server(conn, config)
						reader = bufio.NewReader(conn)
						secure = true
					case strings.HasPrefix(line, "AUTH PLAIN "):
						credentials, e := base64.StdEncoding.DecodeString(strings.TrimSpace(strings.TrimPrefix(line, "AUTH PLAIN ")))
						if e != nil || !secure || string(credentials) != "\x00user\x00password" {
							done <- fmt.Errorf("bad auth")
							return
						}
						authenticated = true
						fmt.Fprint(conn, "235 Authenticated\r\n")
					case strings.HasPrefix(line, "MAIL"), strings.HasPrefix(line, "RCPT"):
						if !authenticated {
							done <- fmt.Errorf("mail before auth")
							return
						}
						fmt.Fprint(conn, "250 OK\r\n")
					case strings.HasPrefix(line, "DATA"):
						fmt.Fprint(conn, "354 Send data\r\n")
						var payload strings.Builder
						for {
							l, e := reader.ReadString('\n')
							if e != nil {
								done <- e
								return
							}
							if l == ".\r\n" {
								break
							}
							payload.WriteString(l)
						}
						if !strings.Contains(payload.String(), "Subject:") || !strings.Contains(payload.String(), "test message") {
							done <- fmt.Errorf("missing message")
							return
						}
						delivered = true
						fmt.Fprint(conn, "250 Queued\r\n")
					case strings.HasPrefix(line, "QUIT"):
						fmt.Fprint(conn, "221 Bye\r\n")
						if !delivered {
							done <- fmt.Errorf("no delivery")
						} else {
							done <- nil
						}
						return
					default:
						done <- fmt.Errorf("unexpected SMTP command")
						return
					}
				}
			}()
			host, port, _ := net.SplitHostPort(listener.Addr().String())
			number, _ := strconv.Atoi(port)
			c := Config{Host: host, Port: number, TLS: mode, Username: "user", From: "sender@example.com"}
			if e := sendTLS(context.Background(), c, "password", "person@example.com", "Test", "test message", &tls.Config{RootCAs: roots, ServerName: host, MinVersion: tls.VersionTLS12}); e != nil {
				t.Fatal(e)
			}
			if e := <-done; e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestShipmentMessagesCoverStorefrontLanguages(t *testing.T) {
	subjects := map[string]bool{}
	for _, language := range []string{"en", "zh-CN", "es", "fr", "pt", "ru", "ar", "id", "vi", "tr", "th"} {
		subject, body := ShipmentMessage(language, "order-reference", "public shipping details")
		if subject == "" || subjects[subject] || !strings.Contains(body, "order-reference") || !strings.Contains(body, "public shipping details") {
			t.Fatalf("shipping translation %s %s %s", language, subject, body)
		}
		subjects[subject] = true
	}
}
