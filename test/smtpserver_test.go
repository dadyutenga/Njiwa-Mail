package test

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"
)

type scenario struct {
	rcpt          string // reply to RCPT (default 250)
	final         string // reply after end of DATA (default 250 queued as TEST123)
	dropAfterData bool   // close the connection after DATA without any reply
	noSTARTTLS    bool
	authFail      bool
	hang          bool // accept TCP then never speak
	cert          *tls.Certificate
}

type fakeSMTP struct {
	ln   net.Listener
	sc   scenario
	mu   sync.Mutex
	cmds []string
	data string
}

func (f *fakeSMTP) saw(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.cmds {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func genCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "127.0.0.1"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, BasicConstraintsValid: true, IsCA: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(c)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}

// startSMTP returns a server and a pool that trusts its certificate.
func startSMTP(t *testing.T, sc scenario) (*fakeSMTP, *x509.CertPool, int) {
	cert, pool := genCert(t)
	if sc.cert == nil {
		sc.cert = &cert
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMTP{ln: ln, sc: sc}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	return f, pool, ln.Addr().(*net.TCPAddr).Port
}

func (f *fakeSMTP) serve(c net.Conn) {
	defer c.Close()
	if f.sc.hang {
		time.Sleep(5 * time.Second)
		return
	}
	var tr *textproto.Conn = textproto.NewConn(c)
	reply := func(s string) { tr.W.WriteString(s + "\r\n"); tr.W.Flush() }
	reply("220 localhost ESMTP test")
	tlsOn := false
	for {
		line, err := tr.ReadLine()
		if err != nil {
			return
		}
		cmd := strings.ToUpper(line)
		f.mu.Lock()
		f.cmds = append(f.cmds, cmd)
		f.mu.Unlock()
		switch {
		case strings.HasPrefix(cmd, "EHLO"):
			if !tlsOn && !f.sc.noSTARTTLS {
				reply("250-localhost\r\n250-STARTTLS\r\n250 AUTH PLAIN")
			} else {
				reply("250-localhost\r\n250 AUTH PLAIN")
			}
		case cmd == "STARTTLS":
			reply("220 ready")
			ts := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{*f.sc.cert}})
			if err := ts.Handshake(); err != nil {
				return
			}
			tr = textproto.NewConn(ts)
			reply = func(s string) { tr.W.WriteString(s + "\r\n"); tr.W.Flush() }
			tlsOn = true
		case strings.HasPrefix(cmd, "AUTH"):
			if f.sc.authFail {
				reply("535 5.7.8 authentication failed")
			} else {
				reply("235 2.7.0 ok")
			}
		case strings.HasPrefix(cmd, "MAIL"):
			reply("250 ok")
		case strings.HasPrefix(cmd, "RCPT"):
			if f.sc.rcpt != "" {
				reply(f.sc.rcpt)
			} else {
				reply("250 ok")
			}
		case cmd == "DATA":
			reply("354 go ahead")
			var sb strings.Builder
			for {
				l, err := tr.ReadLine()
				if err != nil {
					return
				}
				if l == "." {
					break
				}
				sb.WriteString(l + "\n")
			}
			f.mu.Lock()
			f.data = sb.String()
			f.mu.Unlock()
			if f.sc.dropAfterData {
				return
			}
			if f.sc.final != "" {
				reply(f.sc.final)
			} else {
				reply("250 2.0.0 Ok: queued as TEST123")
			}
		case cmd == "QUIT":
			reply("221 bye")
			return
		default:
			reply("250 ok")
		}
	}
}

var _ = bufio.NewReader
var _ = fmt.Sprint
