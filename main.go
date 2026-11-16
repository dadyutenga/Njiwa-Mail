package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/njiwa-tz/mail/internal/cli"
	"github.com/njiwa-tz/mail/internal/devserver"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		if err := devserver.Run(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}
	cli.Run(args)
}
