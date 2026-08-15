package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"obsidian-sync-tunnel/internal/config"
	"obsidian-sync-tunnel/internal/httpapi"
	"obsidian-sync-tunnel/internal/store"
	"obsidian-sync-tunnel/internal/winservice"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: obsidian-sync-server <serve|token|version>")
	}
	switch args[0] {
	case "serve":
		return serve(args[1:])
	case "token":
		return generateToken(args[1:])
	case "version":
		fmt.Println(version)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func generateToken(args []string) error {
	fs := flag.NewFlagSet("token", flag.ContinueOnError)
	bytes := fs.Int("bytes", 32, "number of random bytes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *bytes < 32 {
		return errors.New("token size must be at least 32 bytes")
	}
	value := make([]byte, *bytes)
	if _, err := rand.Read(value); err != nil {
		return fmt.Errorf("generate token: %w", err)
	}
	fmt.Println(base64.RawURLEncoding.EncodeToString(value))
	return nil
}

func serve(args []string) error {
	configPath := findFlagValue(args, "config")
	cfg := config.Default()
	if configPath != "" {
		loaded, err := config.Load(configPath)
		if err != nil {
			return err
		}
		cfg = loaded
	}

	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.StringVar(&configPath, "config", configPath, "path to a JSON configuration file")
	fs.StringVar(&cfg.Listen, "listen", cfg.Listen, "HTTP listen address")
	fs.StringVar(&cfg.DatabasePath, "database", cfg.DatabasePath, "SQLite database path")
	fs.StringVar(&cfg.TokenFile, "token-file", cfg.TokenFile, "file containing the bearer token")
	fs.StringVar(&cfg.LogPath, "log", cfg.LogPath, "optional append-only JSON log path")
	fs.Int64Var(&cfg.MaxUploadBytes, "max-upload-bytes", cfg.MaxUploadBytes, "maximum bytes per file")
	windowsService := fs.Bool("windows-service", false, "run under the Windows Service Control Manager")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	token, err := cfg.ResolveToken()
	if err != nil {
		return err
	}

	db, err := store.Open(cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer db.Close()

	logOutput := io.Writer(os.Stdout)
	var logFile *os.File
	if cfg.LogPath != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.LogPath), 0o700); err != nil {
			return fmt.Errorf("create log directory: %w", err)
		}
		logFile, err = os.OpenFile(cfg.LogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("open log file: %w", err)
		}
		defer logFile.Close()
		if *windowsService {
			logOutput = logFile
		} else {
			logOutput = io.MultiWriter(os.Stdout, logFile)
		}
	}
	logger := slog.New(slog.NewJSONHandler(logOutput, nil))
	if *windowsService {
		return winservice.Run("ObsidianSyncTunnel", func(ctx context.Context) error {
			return runHTTPServer(ctx, cfg, db, token, logger)
		})
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runHTTPServer(ctx, cfg, db, token, logger)
}

func runHTTPServer(ctx context.Context, cfg config.Config, db *store.Store, token string, logger *slog.Logger) error {
	handler := httpapi.New(db, token, cfg.MaxUploadBytes, version, logger)
	server := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
		WriteTimeout:      5 * time.Minute,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("sync server listening", "address", cfg.Listen, "database", cfg.DatabasePath, "version", version)
		errCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve: %w", err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
	}
	return nil
}

func findFlagValue(args []string, name string) string {
	prefix := "--" + name + "="
	for index, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
		if arg == "--"+name && index+1 < len(args) {
			return args[index+1]
		}
	}
	return ""
}
