package inbound

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/mail"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ditronics-tz/mail/internal/secure"
	"github.com/ditronics-tz/mail/internal/service"
)

// IMAPConfig is the catch-all mailbox used when Mailcow cannot relay into the SMTP listener.
type IMAPConfig struct {
	Host, Username, Password, Mailbox string
	Port                              int
}

// PollLoop fetches unseen mail once a minute until ctx is cancelled.
func PollLoop(ctx context.Context, cfg IMAPConfig, svc *service.Service) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		n, err := PollOnce(ctx, cfg, svc)
		if err != nil && ctx.Err() == nil {
			slog.Error("imap poll failed", "err", err)
		} else if n > 0 {
			slog.Info("imap poll stored", "messages", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// PollOnce copies unseen messages into the inbound spool and marks them seen.
func PollOnce(ctx context.Context, cfg IMAPConfig, svc *service.Service) (int, error) {
	if cfg.Port == 0 {
		cfg.Port = 993
	}
	if cfg.Mailbox == "" {
		cfg.Mailbox = "INBOX"
	}
	d := tls.Dialer{Config: &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)))
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	c := &imapConn{r: bufio.NewReader(conn), w: conn}
	if _, err := c.read(); err != nil { // greeting
		return 0, err
	}
	if err := c.cmd(fmt.Sprintf("LOGIN %s %s", imapQuote(cfg.Username), imapQuote(cfg.Password))); err != nil {
		return 0, err
	}
	if err := c.cmd("SELECT " + imapQuote(cfg.Mailbox)); err != nil {
		return 0, err
	}
	lines, err := c.cmdLines("UID SEARCH UNSEEN")
	if err != nil {
		return 0, err
	}
	var uids []string
	for _, line := range lines {
		if strings.HasPrefix(line, "* SEARCH") {
			uids = strings.Fields(strings.TrimPrefix(line, "* SEARCH"))
		}
	}
	stored := 0
	for _, uid := range uids {
		raw, err := c.fetch(uid)
		if err != nil {
			return stored, err
		}
		if err := spoolRaw(svc, raw); err != nil {
			return stored, err
		}
		if err := c.cmd("UID STORE " + uid + ` +FLAGS (\Seen)`); err != nil {
			return stored, err
		}
		stored++
	}
	_ = c.cmd("LOGOUT")
	return stored, nil
}

func spoolRaw(svc *service.Service, raw []byte) error {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	from := firstAddr(msg.Header.Get("From"))
	var to []string
	for _, h := range []string{"To", "Cc", "Delivered-To"} {
		if addr := firstAddr(msg.Header.Get(h)); addr != "" {
			to = append(to, addr)
		}
	}
	if from == "" || len(to) == 0 {
		return fmt.Errorf("imap message has no envelope")
	}
	id := secure.NewUUID()
	dir := filepath.Dir(svc.SpoolFile(id))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := writeSynced(svc.SpoolFile(id), raw); err != nil {
		return err
	}
	return svc.Spool(context.Background(), id, service.KindInbound, from, to, len(raw))
}

func firstAddr(h string) string {
	if strings.TrimSpace(h) == "" {
		return ""
	}
	addrs, err := mail.ParseAddressList(h)
	if err != nil || len(addrs) == 0 {
		return ""
	}
	return addrs[0].Address
}

type imapConn struct {
	r *bufio.Reader
	w io.Writer
	n int
}

func (c *imapConn) cmd(command string) error {
	_, err := c.cmdLines(command)
	return err
}

func (c *imapConn) cmdLines(command string) ([]string, error) {
	c.n++
	tag := fmt.Sprintf("a%03d", c.n)
	if _, err := fmt.Fprintf(c.w, "%s %s\r\n", tag, command); err != nil {
		return nil, err
	}
	var lines []string
	for {
		line, err := c.read()
		if err != nil {
			return nil, err
		}
		lines = append(lines, line)
		if strings.HasPrefix(line, tag+" ") {
			if strings.Contains(line, " OK") {
				return lines, nil
			}
			return lines, fmt.Errorf("imap: %s", line)
		}
	}
}

func (c *imapConn) read() (string, error) {
	line, err := c.r.ReadString('\n')
	return strings.TrimRight(line, "\r\n"), err
}

func (c *imapConn) fetch(uid string) ([]byte, error) {
	c.n++
	tag := fmt.Sprintf("a%03d", c.n)
	if _, err := fmt.Fprintf(c.w, "%s UID FETCH %s RFC822\r\n", tag, uid); err != nil {
		return nil, err
	}
	var raw []byte
	for {
		line, err := c.read()
		if err != nil {
			return nil, err
		}
		if i := strings.Index(line, "{"); i >= 0 && strings.HasSuffix(line, "}") {
			n, err := strconv.Atoi(line[i+1 : len(line)-1])
			if err != nil {
				return nil, err
			}
			raw = make([]byte, n)
			if _, err := io.ReadFull(c.r, raw); err != nil {
				return nil, err
			}
			continue
		}
		if strings.HasPrefix(line, tag+" ") {
			if strings.Contains(line, " OK") && raw != nil {
				return raw, nil
			}
			return nil, fmt.Errorf("imap fetch: %s", line)
		}
	}
}

func imapQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
