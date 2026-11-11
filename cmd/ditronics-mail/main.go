package main

import (
	"os"

	"github.com/ditronics-tz/mail/internal/cli"
)

func main() {
	cli.Run(os.Args[1:])
}
