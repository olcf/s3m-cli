package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/olcf/s3m-cli/internal/cmd/root"
	"github.com/olcf/s3m-cli/internal/runtime"
)

func main() {
	defaultHandler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})
	slog.SetDefault(slog.New(defaultHandler))

	rt, err := runtime.Bootstrap()
	if err != nil {
		slog.Error("unable to start", "error", err)
		os.Exit(1)
	}

	cmd := root.Build(rt, filepath.Base(os.Args[0]))
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}
