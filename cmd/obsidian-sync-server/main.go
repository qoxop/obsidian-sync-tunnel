package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
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
		return errors.New("usage: obsidian-sync-server <serve|token|healthcheck|backup|verify-backup|restore-backup|doctor|version>")
	}
	switch args[0] {
	case "serve":
		return serve(args[1:])
	case "token":
		return generateToken(args[1:])
	case "version":
		fmt.Println(version)
		return nil
	case "healthcheck":
		return healthcheck(args[1:])
	case "backup":
		return backupCommand(args[1:])
	case "verify-backup":
		return verifyBackupCommand(args[1:])
	case "restore-backup":
		return restoreBackupCommand(args[1:])
	case "doctor":
		return doctorCommand(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func backupCommand(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	database := fs.String("database", "data/sync.db", "SQLite database path")
	destination := fs.String("destination", "", "empty backup destination directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *destination == "" {
		return errors.New("--destination is required")
	}
	db, err := store.Open(*database)
	if err != nil {
		return err
	}
	defer db.Close()
	manifest, err := db.Backup(context.Background(), *destination)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(manifest)
}

func verifyBackupCommand(args []string) error {
	fs := flag.NewFlagSet("verify-backup", flag.ContinueOnError)
	directory := fs.String("directory", "", "backup directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *directory == "" {
		return errors.New("--directory is required")
	}
	manifest, err := store.VerifyBackup(*directory)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(manifest)
}

func restoreBackupCommand(args []string) error {
	fs := flag.NewFlagSet("restore-backup", flag.ContinueOnError)
	source := fs.String("source", "", "verified backup directory")
	target := fs.String("target", "", "empty restore target directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *source == "" || *target == "" {
		return errors.New("--source and --target are required")
	}
	return store.RestoreBackup(*source, *target)
}

func doctorCommand(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	database := fs.String("database", "data/sync.db", "SQLite database path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	db, err := store.Open(*database)
	if err != nil {
		return err
	}
	defer db.Close()
	report, err := db.Doctor(context.Background())
	if err != nil {
		return err
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		return err
	}
	if !report.OK {
		return errors.New("doctor found integrity problems")
	}
	return nil
}

func healthcheck(args []string) error {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	url := fs.String("url", "http://127.0.0.1:8787/healthz", "health endpoint URL")
	timeout := fs.Duration("timeout", 5*time.Second, "request timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	client := &http.Client{Timeout: *timeout}
	response, err := client.Get(*url)
	if err != nil {
		return fmt.Errorf("health request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&payload); err != nil {
		return fmt.Errorf("decode health response: %w", err)
	}
	if payload.Status != "ok" {
		return fmt.Errorf("unexpected health status %q", payload.Status)
	}
	return nil
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
	fs.StringVar(&cfg.AdminListen, "admin-listen", cfg.AdminListen, "loopback-only admin HTTP listen address")
	fs.StringVar(&cfg.AdminAuth, "admin-auth", cfg.AdminAuth, "admin authentication mode: none or token")
	fs.StringVar(&cfg.DatabasePath, "database", cfg.DatabasePath, "SQLite database path")
	fs.StringVar(&cfg.AdminTokenFile, "admin-token-file", cfg.AdminTokenFile, "file containing the local admin bearer token")
	fs.StringVar(&cfg.AdminUIDirectory, "admin-ui-directory", cfg.AdminUIDirectory, "directory containing the built admin Web application")
	fs.StringVar(&cfg.BackupDirectory, "backup-directory", cfg.BackupDirectory, "directory containing managed online backups")
	fs.StringVar(&cfg.LogPath, "log", cfg.LogPath, "optional append-only JSON log path")
	fs.Int64Var(&cfg.MaxFileBytes, "max-file-bytes", cfg.MaxFileBytes, "maximum bytes per file")
	fs.Int64Var(&cfg.DefaultVaultQuotaBytes, "default-vault-quota-bytes", cfg.DefaultVaultQuotaBytes, "default logical byte quota per vault; zero disables")
	fs.Int64Var(&cfg.DefaultVaultMaxFiles, "default-vault-max-files", cfg.DefaultVaultMaxFiles, "default file count quota per vault; zero disables")
	fs.Int64Var(&cfg.MinFreeBytes, "min-free-bytes", cfg.MinFreeBytes, "minimum server disk free-space reserve")
	fs.IntVar(&cfg.RateRequestsPerMinute, "rate-requests-per-minute", cfg.RateRequestsPerMinute, "per-device request limit")
	fs.Int64Var(&cfg.RateBytesPerMinute, "rate-bytes-per-minute", cfg.RateBytesPerMinute, "per-device uploaded byte limit")
	fs.BoolVar(&cfg.AllowNonLoopback, "allow-non-loopback", cfg.AllowNonLoopback, "allow binding beyond loopback (for an isolated container network)")
	fs.BoolVar(&cfg.AllowAdminNonLoopback, "allow-admin-non-loopback", cfg.AllowAdminNonLoopback, "allow admin binding inside an isolated container; host publication must remain loopback-only")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	adminToken, err := cfg.ResolveAdminToken()
	if err != nil {
		return err
	}

	db, err := store.Open(cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer db.Close()
	db.ConfigureLimits(store.ResourceLimits{
		MaxFileBytes:      cfg.MaxFileBytes,
		DefaultQuotaBytes: cfg.DefaultVaultQuotaBytes,
		DefaultMaxFiles:   cfg.DefaultVaultMaxFiles,
		MinFreeBytes:      cfg.MinFreeBytes,
	})

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
		logOutput = io.MultiWriter(os.Stdout, logFile)
	}
	logger := slog.New(slog.NewJSONHandler(logOutput, nil))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runHTTPServer(ctx, cfg, db, adminToken, logger)
}

func runHTTPServer(ctx context.Context, cfg config.Config, db *store.Store, adminToken string, logger *slog.Logger) error {
	handler := httpapi.New(db, httpapi.Options{
		MaxFileBytes:      cfg.MaxFileBytes,
		RequestsPerMinute: cfg.RateRequestsPerMinute,
		BytesPerMinute:    cfg.RateBytesPerMinute,
		Version:           version,
	}, logger)
	publicServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
		WriteTimeout:      5 * time.Minute,
	}
	adminServer := &http.Server{
		Addr: cfg.AdminListen,
		Handler: httpapi.NewAdmin(db, adminToken, httpapi.AdminOptions{
			StaticDirectory: cfg.AdminUIDirectory,
			BackupDirectory: cfg.BackupDirectory,
			LogPath:         cfg.LogPath,
			AuthRequired:    cfg.AdminAuth == "token",
		}, logger),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		WriteTimeout:      30 * time.Minute,
	}

	errCh := make(chan error, 2)
	go func() {
		logger.Info("sync server listening", "address", cfg.Listen, "database", cfg.DatabasePath, "version", version)
		errCh <- publicServer.ListenAndServe()
	}()
	go func() {
		logger.Info("admin server listening", "address", cfg.AdminListen)
		errCh <- adminServer.ListenAndServe()
	}()

	var serveErr error
	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			serveErr = fmt.Errorf("serve: %w", err)
		}
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	publicErr := publicServer.Shutdown(shutdownCtx)
	adminErr := adminServer.Shutdown(shutdownCtx)
	if serveErr != nil {
		return serveErr
	}
	if publicErr != nil {
		return fmt.Errorf("shutdown public server: %w", publicErr)
	}
	if adminErr != nil {
		return fmt.Errorf("shutdown admin server: %w", adminErr)
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
