package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sishbi/confluence-mcp/internal/confluence"
	"github.com/sishbi/confluence-mcp/internal/confluencemcp"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	versionFlag := flag.Bool("version", false, "print version information and exit")
	logLevel := flag.String("log-level", "info", "log level: debug, info, warn, error")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("version: %s\ncommit:  %s\ndate:    %s\ngo:      %s\n", version, commit, date, runtime.Version())
		return
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(*logLevel)); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid log level %q: %v\n", *logLevel, err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	}))

	client, err := confluence.New(confluence.Config{
		URL:        requireEnv("CONFLUENCE_URL"),
		Email:      requireEnv("CONFLUENCE_EMAIL"),
		APIToken:   requireEnv("CONFLUENCE_API_TOKEN"),
		MaxRetries: 3,
		BaseDelay:  time.Second,
		Logger:     logger,
	})
	if err != nil {
		logger.Error("failed to create confluence client", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	myself, err := client.GetCurrentUser(ctx)
	if err != nil {
		logger.Error("failed to get current user (check your credentials)", "error", err)
		os.Exit(1)
	}

	s := confluencemcp.NewServer(client, myself, logger)
	if err := s.Run(ctx, &mcp.StdioTransport{}); err != nil {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		fmt.Fprintf(os.Stderr, "Required environment variable %s is not set\n", key)
		os.Exit(1)
	}
	return v
}
