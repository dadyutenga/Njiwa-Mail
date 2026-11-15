// Package objstore is a small client for S3-compatible object storage (AWS S3, MinIO, Ceph,
// Cloudflare R2, Backblaze B2 ...): put, get, delete and delete-by-prefix, signed with AWS
// Signature Version 4. It uses only the standard library. What is stored here is already sealed
// by the caller (AES-GCM), so the service never relies on the provider to keep it confidential.
package objstore

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// ErrNotFound is returned by Get when the object does not exist.
var ErrNotFound = errors.New("objstore: not found")

// Store is what the service needs from an object store. Keys use "/" as a separator.
type Store interface {
	Put(ctx context.Context, key string, data []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
	// DeletePrefix removes every object whose key starts with prefix.
	DeletePrefix(ctx context.Context, prefix string) error
}

type Config struct {
	// Endpoint is the service address, for example https://s3.eu-west-1.amazonaws.com or a MinIO
	// host. Empty means AWS in Region.
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	// KeyPrefix is put in front of every key, so one bucket can serve several installations.
	KeyPrefix string
	// VirtualHosted puts the bucket in the host name (bucket.endpoint) instead of the path.
	VirtualHosted bool
	// AllowHTTP permits a plain-http endpoint (a local MinIO). Off by default.
	AllowHTTP bool
	// Client is the HTTP client; nil uses one with a 60 s timeout.
	Client *http.Client
}

type S3 struct {
	c    Config
	base *url.URL
	hc   *http.Client
	now  func() time.Time
}

// New validates the configuration and returns a client. It makes no request.
func New(c Config) (*S3, error) {
	if c.Bucket == "" || c.AccessKey == "" || c.SecretKey == "" || c.Region == "" {
		return nil, errors.New("objstore: region, bucket, access key and secret key are required")
	}
	if c.Endpoint == "" {
		c.Endpoint = "https://s3." + c.Region + ".amazonaws.com"
	}
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "https" && !(u.Scheme == "http" && c.AllowHTTP)) {
		return nil, errors.New("objstore: the endpoint must be an https address")
	}
	if u.Path != "" && u.Path != "/" {
		return nil, errors.New("objstore: the endpoint must not have a path")
	}
	c.KeyPrefix = strings.Trim(c.KeyPrefix, "/")
	hc := c.Client
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	return &S3{c: c, base: u, hc: hc, now: time.Now}, nil
}

// ---------------------------------------------------------------- signing

// uriEncode is the encoding Signature V4 requires: everything but A-Z a-z 0-9 - _ . ~ is
// percent-encoded; "/" is kept only in paths.
func uriEncode(s string, keepSlash bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		case c == '/' && keepSlash:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

func hexSHA256(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// canonicalQuery sorts and encodes query parameters as Signature V4 wants.
func canonicalQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vs := append([]string(nil), q[k]...)
		sort.Strings(vs)
		for _, v := range vs {
			parts = append(parts, uriEncode(k, false)+"="+uriEncode(v, false))
		}
	}
	return strings.Join(parts, "&")
}

// signRequest adds x-amz-date, x-amz-content-sha256 and Authorization. Every header already on the
// request (and Host) is signed. encodedPath and rawQuery must be what is sent on the wire.
func signRequest(req *http.Request, encodedPath, rawQuery, payloadHash, region, access, secret string, now time.Time) {
	amzDate := now.UTC().Format("20060102T150405Z")
	date := now.UTC().Format("20060102")
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	type hv struct{ k, v string }
	hs := []hv{{"host", host}}
	for k, vs := range req.Header {
		hs = append(hs, hv{strings.ToLower(k), strings.Join(strings.Fields(strings.Join(vs, ",")), " ")})
	}
	sort.Slice(hs, func(i, j int) bool { return hs[i].k < hs[j].k })
	var canon, signed strings.Builder
	for i, h := range hs {
		canon.WriteString(h.k + ":" + h.v + "\n")
		if i > 0 {
			signed.WriteByte(';')
		}
		signed.WriteString(h.k)
	}
	canonical := strings.Join([]string{req.Method, encodedPath, rawQuery, canon.String(), signed.String(), payloadHash}, "\n")
	scope := date + "/" + region + "/s3/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hexSHA256([]byte(canonical))
	k := hmacSHA256([]byte("AWS4"+secret), date)
	k = hmacSHA256(k, region)
	k = hmacSHA256(k, "s3")
	k = hmacSHA256(k, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(k, toSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+access+"/"+scope+", SignedHeaders="+signed.String()+", Signature="+sig)
}

// ---------------------------------------------------------------- requests

func (s *S3) fullKey(key string) string {
	if s.c.KeyPrefix == "" {
		return key
	}
	return s.c.KeyPrefix + "/" + key
}

// target builds the URL for an object key (or the bucket when key is empty).
func (s *S3) target(key string, q url.Values) (host, encPath, rawQuery string) {
	host = s.base.Host
	p := "/"
	if s.c.VirtualHosted {
		host = s.c.Bucket + "." + s.base.Host
		if key != "" {
			p = "/" + key
		}
	} else {
		p = "/" + s.c.Bucket
		if key != "" {
			p += "/" + key
		}
	}
	return host, uriEncode(p, true), canonicalQuery(q)
}

func (s *S3) do(ctx context.Context, method, key string, q url.Values, body []byte) (*http.Response, error) {
	host, encPath, rawQuery := s.target(key, q)
	u := url.URL{Scheme: s.base.Scheme, Host: host, RawPath: encPath, RawQuery: rawQuery}
	u.Path, _ = url.PathUnescape(encPath)
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Host = host
	req.ContentLength = int64(len(body))
	signRequest(req, encPath, rawQuery, hexSHA256(body), s.c.Region, s.c.AccessKey, s.c.SecretKey, s.now())
	return s.hc.Do(req)
}

func drain(r *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	r.Body.Close()
}

// errorFrom turns an unexpected answer into an error without including the response body, which
// could echo request details.
func errorFrom(op string, r *http.Response) error {
	defer drain(r)
	var e struct {
		Code string `xml:"Code"`
	}
	_ = xml.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&e)
	code := e.Code
	if len(code) > 64 || strings.ContainsAny(code, " \r\n") {
		code = ""
	}
	return fmt.Errorf("objstore: %s answered %d %s", op, r.StatusCode, code)
}

func (s *S3) Put(ctx context.Context, key string, data []byte) error {
	r, err := s.do(ctx, "PUT", s.fullKey(key), nil, data)
	if err != nil {
		return fmt.Errorf("objstore: put: %w", sanitize(err))
	}
	if r.StatusCode/100 != 2 {
		return errorFrom("put", r)
	}
	drain(r)
	return nil
}

func (s *S3) Get(ctx context.Context, key string) ([]byte, error) {
	r, err := s.do(ctx, "GET", s.fullKey(key), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("objstore: get: %w", sanitize(err))
	}
	if r.StatusCode == 404 {
		drain(r)
		return nil, ErrNotFound
	}
	if r.StatusCode != 200 {
		return nil, errorFrom("get", r)
	}
	defer r.Body.Close()
	b, err := io.ReadAll(io.LimitReader(r.Body, 512<<20))
	if err != nil {
		return nil, fmt.Errorf("objstore: get: %w", sanitize(err))
	}
	return b, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	r, err := s.do(ctx, "DELETE", s.fullKey(key), nil, nil)
	if err != nil {
		return fmt.Errorf("objstore: delete: %w", sanitize(err))
	}
	if r.StatusCode/100 != 2 && r.StatusCode != 404 {
		return errorFrom("delete", r)
	}
	drain(r)
	return nil
}

type listResult struct {
	IsTruncated bool     `xml:"IsTruncated"`
	NextToken   string   `xml:"NextContinuationToken"`
	Keys        []string `xml:"Contents>Key"`
}

func (s *S3) DeletePrefix(ctx context.Context, prefix string) error {
	token := ""
	for page := 0; page < 1000; page++ {
		q := url.Values{"list-type": {"2"}, "prefix": {s.fullKey(prefix)}}
		if token != "" {
			q.Set("continuation-token", token)
		}
		r, err := s.do(ctx, "GET", "", q, nil)
		if err != nil {
			return fmt.Errorf("objstore: list: %w", sanitize(err))
		}
		if r.StatusCode != 200 {
			return errorFrom("list", r)
		}
		var lr listResult
		err = xml.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&lr)
		r.Body.Close()
		if err != nil {
			return errors.New("objstore: list: the answer could not be read")
		}
		for _, k := range lr.Keys {
			// Keys come back with the key prefix already in them.
			dr, err := s.do(ctx, "DELETE", k, nil, nil)
			if err != nil {
				return fmt.Errorf("objstore: delete: %w", sanitize(err))
			}
			if dr.StatusCode/100 != 2 && dr.StatusCode != 404 {
				return errorFrom("delete", dr)
			}
			drain(dr)
		}
		if !lr.IsTruncated || lr.NextToken == "" {
			return nil
		}
		token = lr.NextToken
	}
	return errors.New("objstore: too many objects under one prefix")
}

// sanitize drops the URL from a transport error: it contains the bucket and key names.
func sanitize(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
