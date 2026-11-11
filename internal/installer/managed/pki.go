package managed

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"
)

// The submission listener uses mutual TLS: Postfix only relays for a client
// presenting the certificate the installer issued to the Njiwa worker. No
// network range is trusted, and no SASL daemon is needed.

type certPair struct{ CertPEM, KeyPEM []byte }

func serial() *big.Int {
	n, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	return n
}

func keyPEM(k *rsa.PrivateKey) []byte {
	der, _ := x509.MarshalPKCS8PrivateKey(k)
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func newCA(now time.Time) (certPair, *x509.Certificate, *rsa.PrivateKey, error) {
	k, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return certPair{}, nil, nil, err
	}
	tpl := &x509.Certificate{SerialNumber: serial(), Subject: pkix.Name{CommonName: "Njiwa-Mail local submission CA"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(10, 0, 0), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, MaxPathLenZero: true}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		return certPair{}, nil, nil, err
	}
	c, _ := x509.ParseCertificate(der)
	return certPair{pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), keyPEM(k)}, c, k, nil
}

func issue(ca *x509.Certificate, caKey *rsa.PrivateKey, cn string, dns []string, ips []net.IP, client bool, now time.Time) (certPair, error) {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return certPair{}, err
	}
	usage := []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	if client {
		usage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	tpl := &x509.Certificate{SerialNumber: serial(), Subject: pkix.Name{CommonName: cn}, DNSNames: dns, IPAddresses: ips,
		NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(5, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: usage}
	der, err := x509.CreateCertificate(rand.Reader, tpl, ca, &k.PublicKey, caKey)
	if err != nil {
		return certPair{}, err
	}
	return certPair{pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), keyPEM(k)}, nil
}

func parseCA(certPEM, keyPEM []byte) (*x509.Certificate, *rsa.PrivateKey, error) {
	cb, _ := pem.Decode(certPEM)
	kb, _ := pem.Decode(keyPEM)
	if cb == nil || kb == nil {
		return nil, nil, errors.New("CA files are not PEM")
	}
	c, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, nil, err
	}
	k, err := x509.ParsePKCS8PrivateKey(kb.Bytes)
	if err != nil {
		return nil, nil, err
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, nil, errors.New("CA key is not RSA")
	}
	return c, rk, nil
}

// Fingerprint is the colon-separated upper-case SHA-256 digest of a
// certificate, as Postfix prints it with smtpd_tls_fingerprint_digest=sha256.
func Fingerprint(certPEM []byte) (string, error) {
	b, _ := pem.Decode(certPEM)
	if b == nil {
		return "", errors.New("certificate is not PEM")
	}
	sum := sha256.Sum256(b.Bytes)
	parts := make([]string, len(sum))
	for i, v := range sum {
		parts[i] = fmt.Sprintf("%02X", v)
	}
	return strings.Join(parts, ":"), nil
}

// newDKIMKey returns a 2048-bit RSA key (PKCS#1 PEM, the format OpenDKIM
// reads) and its DNS TXT value.
func newDKIMKey() (privPEM []byte, txt string, err error) {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, "", err
	}
	privPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
	txt, err = dkimTXT(privPEM)
	return privPEM, txt, err
}

// dkimTXT derives the public DNS record from an existing private key file, so
// a re-run republishes exactly the key OpenDKIM signs with.
func dkimTXT(privPEM []byte) (string, error) {
	b, _ := pem.Decode(privPEM)
	if b == nil {
		return "", errors.New("DKIM key is not PEM")
	}
	var pub *rsa.PublicKey
	if k, err := x509.ParsePKCS1PrivateKey(b.Bytes); err == nil {
		pub = &k.PublicKey
	} else if k8, err := x509.ParsePKCS8PrivateKey(b.Bytes); err == nil {
		rk, ok := k8.(*rsa.PrivateKey)
		if !ok {
			return "", errors.New("DKIM key is not RSA")
		}
		pub = &rk.PublicKey
	} else {
		return "", errors.New("DKIM key cannot be parsed")
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", err
	}
	return "v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(der), nil
}
