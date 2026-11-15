package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func baseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	t.Setenv("HMAC_KEY", base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
}

func TestAttachmentStoreSettings(t *testing.T) {
	baseEnv(t)
	c, err := Load()
	if err != nil || c.AttachmentStore != "disk" {
		t.Fatalf("default: %v %v", c, err)
	}

	t.Setenv("ATTACHMENT_STORE", "ftp")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "disk or s3") {
		t.Fatalf("unknown store: %v", err)
	}

	t.Setenv("ATTACHMENT_STORE", "s3")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "S3_BUCKET") {
		t.Fatalf("s3 without settings: %v", err)
	}

	secretFile := filepath.Join(t.TempDir(), "s3-secret")
	if err := os.WriteFile(secretFile, []byte("file-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("S3_REGION", "eu-west-1")
	t.Setenv("S3_BUCKET", "mail")
	t.Setenv("S3_ACCESS_KEY_ID", "AKID")
	t.Setenv("S3_SECRET_ACCESS_KEY_FILE", secretFile)
	t.Setenv("S3_KEY_PREFIX", "njiwa/")
	c, err = Load()
	if err != nil || c.S3.SecretKey != "file-secret" || c.S3.Bucket != "mail" || c.S3.KeyPrefix != "njiwa/" || c.S3.VirtualHosted || c.S3.AllowHTTP {
		t.Fatalf("s3: %+v %v", c, err)
	}

	t.Setenv("S3_VIRTUAL_HOSTED", "maybe")
	if _, err := Load(); err == nil {
		t.Fatal("a bad boolean was accepted")
	}
}
