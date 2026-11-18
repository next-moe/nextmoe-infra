// ops-alert mails one maintenance alert from the site's own SMTP account,
// with the settings every service reads (environment, then the config store):
//
//	ops-alert -to <addr> -subject <s> [-body <file>|-] [-note <line>]
package main

import (
	"flag"
	"fmt"
	"html"
	"io"
	"log/slog"
	"os"
	"strings"

	"api/internal/infrastructure/mail"
	"api/pkg/config"
)

const keepLines = 200

func main() {
	to := flag.String("to", "", "recipient")
	subject := flag.String("subject", "", "subject line")
	bodyPath := flag.String("body", "-", "file holding the body, or - for stdin")
	note := flag.String("note", "", "a line set above the body")
	flag.Parse()
	if *to == "" || *subject == "" {
		fmt.Fprintln(os.Stderr, "usage: ops-alert -to <addr> -subject <s> [-body <file>|-] [-note <line>]")
		os.Exit(2)
	}
	if err := run(*to, *subject, *bodyPath, *note); err != nil {
		slog.Error("ops-alert", "error", err)
		os.Exit(1)
	}
	fmt.Println("alert sent to " + *to)
}

func run(to, subject, bodyPath, note string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.Mail.Host == "" || cfg.Mail.Account == "" || cfg.Mail.Password == "" {
		return fmt.Errorf("SMTP settings incomplete (host=%t account=%t password=%t)",
			cfg.Mail.Host != "", cfg.Mail.Account != "", cfg.Mail.Password != "")
	}
	var raw []byte
	if bodyPath == "-" {
		raw, err = io.ReadAll(os.Stdin)
	} else {
		raw, err = os.ReadFile(bodyPath)
	}
	if err != nil {
		return err
	}
	return mail.NewMailer(cfg.Mail).SendEmail(to, subject, render(note, string(raw)))
}

// render keeps the tail, where a failing run says why it died, small enough
// to read on a phone.
func render(note, body string) string {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if len(lines) > keepLines {
		lines = append([]string{fmt.Sprintf("[... %d earlier lines omitted ...]", len(lines)-keepLines)}, lines[len(lines)-keepLines:]...)
	}
	var b strings.Builder
	if note != "" {
		b.WriteString("<p><b>" + html.EscapeString(note) + "</b></p>")
	}
	b.WriteString(`<pre style="white-space:pre-wrap;font:12px/1.4 monospace">`)
	b.WriteString(html.EscapeString(strings.Join(lines, "\n")))
	b.WriteString("</pre>")
	return b.String()
}
