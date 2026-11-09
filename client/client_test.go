package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetriesSameIdempotencyKeyOn5xxAndStops(t *testing.T) {
	var n int32
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		if atomic.AddInt32(&n, 1) < 3 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(202)
		json.NewEncoder(w).Encode(map[string]string{"id": "abc", "status": "queued"})
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", Backoff: time.Millisecond}
	a, err := c.Send(context.Background(), "evt-1", Email{From: "a@b.co", To: "c@d.co", Template: "otp", TemplateVersion: 1})
	if err != nil || a.ID != "abc" {
		t.Fatal(a, err)
	}
	if len(keys) != 3 || keys[0] != "evt-1" || keys[1] != "evt-1" || keys[2] != "evt-1" {
		t.Fatalf("idempotency key must be identical across retries: %v", keys)
	}
}

func TestNoRetryOnClientError(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		w.WriteHeader(422)
		w.Write([]byte(`{"error":{"code":"unknown_variable"}}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", Backoff: time.Millisecond}
	_, err := c.Send(context.Background(), "evt-2", Email{})
	var ae *APIError
	if !errors.As(err, &ae) || ae.Code != "unknown_variable" || ae.Status != 422 || n != 1 {
		t.Fatalf("err=%v calls=%d", err, n)
	}
}

func TestGivesUpAfterRetriesAndHonorsRetryAfter(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(429)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, APIKey: "k", MaxRetries: 2, Backoff: time.Millisecond}
	_, err := c.Send(context.Background(), "evt-3", Email{})
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 429 || n != 3 {
		t.Fatalf("err=%v calls=%d", err, n)
	}
}

func TestStatusAndAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`{"id":"x","status":"submitted","attempts":1}`))
	}))
	defer srv.Close()
	s, err := (&Client{BaseURL: srv.URL, APIKey: "secret"}).Status(context.Background(), "x")
	if err != nil || s.Status != "submitted" {
		t.Fatal(s, err)
	}
	if NewIdempotencyKey("otp") == NewIdempotencyKey("otp") {
		t.Fatal("keys must be unique")
	}
}
