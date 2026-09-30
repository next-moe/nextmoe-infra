package mail

import (
	"bufio"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"api/pkg/config"
)

type smtpSink struct {
	mu   sync.Mutex
	rcpt []string
	data string
}

func startFakeSMTP(t *testing.T, sink *smtpSink) (host string, port int, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go serveSMTP(c, sink)
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port, func() {
		_ = ln.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
}

func serveSMTP(c net.Conn, sink *smtpSink) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(c)
	w := bufio.NewWriter(c)
	line := func(s string) {
		_, _ = w.WriteString(s + "\r\n")
		_ = w.Flush()
	}
	line("220 localhost ESMTP")
	inData := false
	var body strings.Builder
	for {
		s, err := r.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				return
			}
			return
		}
		if inData {
			if s == ".\r\n" || s == ".\n" {
				inData = false
				sink.mu.Lock()
				sink.data = body.String()
				sink.mu.Unlock()
				line("250 OK")
				continue
			}
			body.WriteString(s)
			continue
		}
		upper := strings.ToUpper(strings.TrimSpace(s))
		switch {
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			line("250-localhost")
			line("250 OK")
		case strings.HasPrefix(upper, "MAIL"):
			line("250 OK")
		case strings.HasPrefix(upper, "RCPT"):
			sink.mu.Lock()
			sink.rcpt = append(sink.rcpt, strings.TrimSpace(s))
			sink.mu.Unlock()
			line("250 OK")
		case strings.HasPrefix(upper, "DATA"):
			line("354 End data with <CR><LF>.<CR><LF>")
			inData = true
		case strings.HasPrefix(upper, "QUIT"):
			line("221 bye")
			return
		default:
			line("250 OK")
		}
	}
}

func TestSendShelled(t *testing.T) {
	sink := &smtpSink{}
	host, port, stop := startFakeSMTP(t, sink)
	defer stop()
	m := NewMailer(config.MailConfig{
		From:    "NextMoe",
		Host:    host,
		Port:    port,
		Account: "noreply@localhost",
	})
	if err := m.SendShelled("ops@example.com", "subject-here", "HeadingX", `<p id="inner">INNER-HTML</p>`); err != nil {
		t.Fatal(err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if !strings.Contains(strings.Join(sink.rcpt, "\n"), "ops@example.com") {
		t.Fatalf("rcpt=%v", sink.rcpt)
	}
	if !strings.Contains(sink.data, "HeadingX") {
		t.Fatalf("heading missing: %s", sink.data)
	}
	if !strings.Contains(sink.data, `<p id="inner">INNER-HTML</p>`) {
		t.Fatalf("inner missing: %s", sink.data)
	}
	if !strings.Contains(sink.data, "<!DOCTYPE html>") || !strings.Contains(sink.data, "NextMoe") {
		t.Fatalf("shell missing: %s", sink.data)
	}
}
