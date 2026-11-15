package objstore

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// The "GET Object" example from the AWS Signature Version 4 documentation. If our signer produces
// the documented signature, requests are signed the way S3 expects.
func TestSignatureMatchesAWSDocumentedExample(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	req.Header.Set("Range", "bytes=0-9")
	now := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
	signRequest(req, "/test.txt", "", hexSHA256(nil), "us-east-1", "AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", now)
	got := req.Header.Get("Authorization")
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request,SignedHeaders=host;range;x-amz-content-sha256;x-amz-date,Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if strings.ReplaceAll(got, ", ", ",") != want {
		t.Fatalf("authorization\n got %s\nwant %s", got, want)
	}
}

func TestURIEncodeAndQuery(t *testing.T) {
	if got := uriEncode("a b/c+d~é", true); got != "a%20b/c%2Bd~%C3%A9" {
		t.Fatalf("path: %s", got)
	}
	if got := uriEncode("a/b", false); got != "a%2Fb" {
		t.Fatalf("query: %s", got)
	}
}

// fakeS3 is an in-memory S3: path-style, PUT/GET/DELETE and ListObjectsV2 with paging. It checks
// that every request carries a well-formed Signature V4 Authorization header.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
	bucket  string
	badAuth int
	pageMax int
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if a := r.Header.Get("Authorization"); !strings.HasPrefix(a, "AWS4-HMAC-SHA256 Credential=AKID/") || !strings.Contains(a, "SignedHeaders=host;x-amz-content-sha256;x-amz-date") ||
		r.Header.Get("x-amz-date") == "" || len(r.Header.Get("x-amz-content-sha256")) != 64 {
		f.badAuth++
		http.Error(w, "<Error><Code>AccessDenied</Code></Error>", 403)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.EscapedPath(), "/"+f.bucket)
	path = strings.TrimPrefix(path, "/")
	switch {
	case r.Method == "GET" && path == "" && r.URL.Query().Get("list-type") == "2":
		prefix := r.URL.Query().Get("prefix")
		var keys []string
		for k := range f.objects {
			if strings.HasPrefix(k, prefix) {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		// Like S3, the continuation token names the last key of the previous page, so deleting
		// while paging does not skip anything.
		start := 0
		if tok := r.URL.Query().Get("continuation-token"); tok != "" {
			start = sort.SearchStrings(keys, tok)
			for start < len(keys) && keys[start] <= tok {
				start++
			}
		}
		end := len(keys)
		res := listResult{}
		if f.pageMax > 0 && start+f.pageMax < end {
			end = start + f.pageMax
			res.IsTruncated, res.NextToken = true, keys[end-1]
		}
		res.Keys = keys[start:end]
		type contents struct {
			Key string `xml:"Key"`
		}
		var out struct {
			XMLName     xml.Name   `xml:"ListBucketResult"`
			IsTruncated bool       `xml:"IsTruncated"`
			Next        string     `xml:"NextContinuationToken"`
			Contents    []contents `xml:"Contents"`
		}
		out.IsTruncated, out.Next = res.IsTruncated, res.NextToken
		for _, k := range res.Keys {
			out.Contents = append(out.Contents, contents{k})
		}
		_ = xml.NewEncoder(w).Encode(out)
	case r.Method == "PUT":
		b, _ := io.ReadAll(r.Body)
		if hexSHA256(b) != r.Header.Get("x-amz-content-sha256") {
			http.Error(w, "<Error><Code>XAmzContentSHA256Mismatch</Code></Error>", 400)
			return
		}
		f.objects[path] = b
		w.WriteHeader(200)
	case r.Method == "GET":
		b, ok := f.objects[path]
		if !ok {
			http.Error(w, "<Error><Code>NoSuchKey</Code></Error>", 404)
			return
		}
		_, _ = w.Write(b)
	case r.Method == "DELETE":
		delete(f.objects, path)
		w.WriteHeader(204)
	default:
		http.Error(w, "x", 400)
	}
}

func newFake(t *testing.T) (*S3, *fakeS3) {
	t.Helper()
	f := &fakeS3{objects: map[string][]byte{}, bucket: "mail-bucket"}
	srv := httptest.NewTLSServer(f)
	t.Cleanup(srv.Close)
	s, err := New(Config{Endpoint: srv.URL, Region: "eu-west-1", Bucket: "mail-bucket", AccessKey: "AKID", SecretKey: "secret", KeyPrefix: "njiwa/", Client: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return s, f
}

func TestPutGetDeleteAndPrefix(t *testing.T) {
	s, f := newFake(t)
	ctx := context.Background()
	f.pageMax = 2
	for _, k := range []string{"attachments/m1/00-a", "attachments/m1/01-b", "attachments/m1/02-c", "attachments/m2/00-d"} {
		if err := s.Put(ctx, k, []byte("sealed "+k)); err != nil {
			t.Fatal(err)
		}
	}
	// Keys live under the configured prefix.
	if _, ok := f.objects["njiwa/attachments/m1/00-a"]; !ok {
		t.Fatalf("prefix not applied: %v", f.objects)
	}
	b, err := s.Get(ctx, "attachments/m2/00-d")
	if err != nil || string(b) != "sealed attachments/m2/00-d" {
		t.Fatalf("get: %q %v", b, err)
	}
	if _, err := s.Get(ctx, "attachments/none/x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	// Prefix delete walks every page and leaves other prefixes alone ("m1/" must not match "m10/").
	f.objects["njiwa/attachments/m10/00-z"] = []byte("other")
	if err := s.DeletePrefix(ctx, "attachments/m1/"); err != nil {
		t.Fatal(err)
	}
	var left []string
	for k := range f.objects {
		left = append(left, k)
	}
	sort.Strings(left)
	if strings.Join(left, ",") != "njiwa/attachments/m10/00-z,njiwa/attachments/m2/00-d" {
		t.Fatalf("left: %v", left)
	}
	if err := s.Delete(ctx, "attachments/m2/00-d"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "attachments/m2/00-d"); err != nil {
		t.Fatalf("deleting twice must be fine: %v", err)
	}
	if f.badAuth != 0 {
		t.Fatalf("%d unsigned requests", f.badAuth)
	}
}

func TestErrorsDoNotLeakNamesAndConfigIsChecked(t *testing.T) {
	s, f := newFake(t)
	f.badAuth = 0
	// A wrong-signature style failure is an error with the provider's code, never the key name.
	s.c.AccessKey = "WRONG"
	err := s.Put(context.Background(), "attachments/secret-name/file", []byte("x"))
	if err == nil || strings.Contains(err.Error(), "secret-name") || !strings.Contains(err.Error(), "403") {
		t.Fatalf("error: %v", err)
	}
	for name, c := range map[string]Config{
		"no bucket": {Region: "r", AccessKey: "a", SecretKey: "s"},
		"http":      {Region: "r", Bucket: "b", AccessKey: "a", SecretKey: "s", Endpoint: "http://example.org"},
		"path":      {Region: "r", Bucket: "b", AccessKey: "a", SecretKey: "s", Endpoint: "https://example.org/x"},
	} {
		if _, err := New(c); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	if _, err := New(Config{Region: "r", Bucket: "b", AccessKey: "a", SecretKey: "s", Endpoint: "http://localhost:9000", AllowHTTP: true}); err != nil {
		t.Fatalf("local http with the flag: %v", err)
	}
	aws, err := New(Config{Region: "eu-west-1", Bucket: "b", AccessKey: "a", SecretKey: "s"})
	if err != nil || aws.base.Host != "s3.eu-west-1.amazonaws.com" {
		t.Fatalf("default endpoint: %v %v", aws, err)
	}
}
