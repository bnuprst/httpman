//go:build nogui

package main

import (
	"errors"
	"os"

	"github.com/bnuprst/httpman/internal/cli"
)

func runGUI(args []string) error {
	cli.Main([]string{"help"}, os.Stdout, os.Stderr)
	return errors.New("this build has no desktop UI (built with -tags nogui)")
}
