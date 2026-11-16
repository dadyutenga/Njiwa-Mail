// Package devserver starts the API and dashboard together for local development.
package devserver

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/njiwa-tz/mail/internal/config"
)

// Run uses the root .env for the API and Vite's own environment files for the UI.
// The database must already be running and migrated.
func Run(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return fmt.Errorf("dashboard requires Node.js: %w", err)
	}
	dashboard := filepath.Join("apps", "dashboard")
	if _, err := os.Stat(filepath.Join(dashboard, "node_modules", "vite", "bin", "vite.js")); err != nil {
		return fmt.Errorf("dashboard dependencies are missing; run: cd apps/dashboard && npm ci")
	}
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	proxy, err := apiURL(cfg.ListenAddr)
	if err != nil {
		return err
	}
	port := os.Getenv("NJIWA_DASHBOARD_PORT")
	if port == "" {
		port = "5173"
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("NJIWA_DASHBOARD_PORT must be a port between 1 and 65535")
	}
	fmt.Fprintf(os.Stdout, "Starting backend at %s and dashboard at http://127.0.0.1:%s\nPress Ctrl+C to stop both.\n", proxy, port)
	return supervise(ctx, []process{
		{name: "backend", args: []string{binary, "serve"}, env: []string{"NJIWA_LOCAL_DISCOVERY=true"}},
		{name: "dashboard", args: []string{node, "node_modules/vite/bin/vite.js", "--host", "127.0.0.1", "--port", port}, dir: dashboard,
			env: []string{"VITE_API_PROXY=" + proxy}},
	})
}

func apiURL(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("invalid LISTEN_ADDR: %w", err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port), nil
}

type process struct {
	name string
	args []string
	dir  string
	env  []string
}

// supervise stops and reaps every child on cancellation, startup failure, or exit.
func supervise(ctx context.Context, processes []process) error {
	children, cancel := context.WithCancel(ctx)
	results := make(chan error, len(processes))
	running := 0
	defer func() {
		cancel()
		for range running {
			<-results
		}
	}()
	for _, p := range processes {
		cmd := exec.CommandContext(children, p.args[0], p.args[1:]...)
		cmd.Dir = p.dir
		cmd.Env = append(os.Environ(), p.env...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
		// The API has up to 15 seconds to finish in-flight requests.
		cmd.WaitDelay = 20 * time.Second
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start %s: %w", p.name, err)
		}
		running++
		go func() {
			err := cmd.Wait()
			if err == nil {
				results <- fmt.Errorf("%s stopped unexpectedly", p.name)
			} else {
				results <- fmt.Errorf("%s stopped: %w", p.name, err)
			}
		}()
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-results:
		running--
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
}
