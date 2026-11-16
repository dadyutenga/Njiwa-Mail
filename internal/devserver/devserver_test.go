package devserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAPIURL(t *testing.T) {
	for addr, want := range map[string]string{
		"127.0.0.1:8090": "http://127.0.0.1:8090",
		"0.0.0.0:8080":   "http://127.0.0.1:8080",
		":8080":          "http://127.0.0.1:8080",
		"[::]:8080":      "http://127.0.0.1:8080",
		"[::1]:8090":     "http://[::1]:8090",
	} {
		got, err := apiURL(addr)
		if err != nil || got != want {
			t.Fatalf("apiURL(%q) = %q, %v; want %q", addr, got, err, want)
		}
	}
	if _, err := apiURL("invalid"); err == nil {
		t.Fatal("invalid listen address accepted")
	}
}

func TestSupervise(t *testing.T) {
	for _, mode := range []string{"cancel", "exit", "start-failure"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ready := filepath.Join(t.TempDir(), "ready")
			helper := process{name: "helper", args: []string{os.Args[0], "-test.run=^TestDevProcess$"},
				env: []string{"NJIWA_DEV_TEST_READY=" + ready}}
			processes := []process{helper}
			if mode == "exit" {
				exit := helper
				exit.name = "exiting helper"
				exit.env = append([]string{}, helper.env...)
				exit.env = append(exit.env, "NJIWA_DEV_TEST_EXIT=1")
				processes = append(processes, exit)
			} else if mode == "start-failure" {
				processes = append(processes, process{name: "missing", args: []string{filepath.Join(t.TempDir(), "missing")}})
			}
			finished := make(chan error, 1)
			go func() { finished <- supervise(ctx, processes) }()
			if mode == "cancel" {
				deadline := time.Now().Add(5 * time.Second)
				for {
					if _, err := os.Stat(ready); err == nil {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("child did not start")
					}
					time.Sleep(10 * time.Millisecond)
				}
				cancel()
			}
			select {
			case err := <-finished:
				if mode == "cancel" && err != nil {
					t.Fatal(err)
				}
				if mode == "exit" && (err == nil || !strings.Contains(err.Error(), "exiting helper")) {
					t.Fatalf("expected child failure, got %v", err)
				}
				if mode == "start-failure" && (err == nil || !strings.Contains(err.Error(), "start missing")) {
					t.Fatalf("expected startup failure, got %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("supervisor failed to stop and reap children")
			}
		})
	}
}

func TestDevProcess(t *testing.T) {
	ready := os.Getenv("NJIWA_DEV_TEST_READY")
	if ready == "" {
		return
	}
	if os.Getenv("NJIWA_DEV_TEST_EXIT") != "" {
		for {
			if _, err := os.Stat(ready); err == nil {
				os.Exit(7)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if err := os.WriteFile(ready, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for {
		time.Sleep(time.Second)
	}
}
