// Command server is the FloatTranslate Go backend sidecar.
//
// Startup contract (docs/02 §5):
//   - binds 127.0.0.1 only, port from FT_HTTP_PORT ("0" = ephemeral)
//   - opens SQLite, applies migrations (any failure aborts startup)
//   - after listening prints EXACTLY ONE JSON line to stdout:
//     {"status":"ready","port":<actual port>,"version":"1.0.0"}
//     which the Tauri host parses. All logging goes to stderr and log files.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/Weber-5/FloatTranslate/backend/internal/api"
	"github.com/Weber-5/FloatTranslate/backend/internal/config"
	"github.com/Weber-5/FloatTranslate/backend/internal/database"
	"github.com/Weber-5/FloatTranslate/backend/internal/logging"
	"github.com/Weber-5/FloatTranslate/backend/internal/migration"
	"github.com/Weber-5/FloatTranslate/backend/internal/repository"
	"github.com/Weber-5/FloatTranslate/backend/internal/terminology"
	"github.com/Weber-5/FloatTranslate/backend/internal/translation"
)

func main() {
	if err := run(); err != nil {
		// stderr only — stdout is reserved for the single ready line.
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	redactor := logging.NewRedactor()
	redactor.Register(cfg.SessionToken)
	logger := newLogger(cfg.DataRoot, redactor)
	slog.SetDefault(logger)

	db, err := database.Open(filepath.Join(cfg.DataRoot, "floattranslate.db"))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	// Migrations run before anything else; failure aborts startup with
	// MIGRATION_FAILED and no business writes may happen.
	if err := migration.Run(db, migration.Embedded()); err != nil {
		return err
	}

	settingsRepo := repository.NewSettingsRepo(db)
	historyRepo := repository.NewHistoryRepo(db)
	cacheRepo := repository.NewCacheRepo(db)
	tabsRepo := repository.NewTabsRepo(db)
	termsSvc, err := terminology.NewService(context.Background(), repository.NewTerminologyRepo(db))
	if err != nil {
		return fmt.Errorf("load terminology: %w", err)
	}

	provider := translation.NewMockProvider()
	providerSettings := api.NewProviderSettingsStore(settingsRepo)
	pipeline, err := translation.NewPipeline(provider, providerSettings.TranslationModel,
		termsSvc, cacheRepo, historyRepo)
	if err != nil {
		return fmt.Errorf("build pipeline: %w", err)
	}

	server := api.NewServer(cfg, logger, pipeline, settingsRepo, historyRepo, tabsRepo,
		termsSvc, provider, db.Ping)

	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", cfg.HTTPPort))
	if err != nil {
		return fmt.Errorf("listen on 127.0.0.1:%s: %w", cfg.HTTPPort, err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	// The one and only stdout line — parsed by the Tauri host.
	fmt.Printf("{\"status\":\"ready\",\"port\":%d,\"version\":\"%s\"}\n", port, cfg.Version)

	httpServer := &http.Server{
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return httpServer.Serve(listener)
}

// newLogger builds the redacted JSON logger writing to stderr and
// <DataRoot>/logs/server.log. stdout is never used for logging.
func newLogger(dataRoot string, redactor *logging.Redactor) *slog.Logger {
	writers := []io.Writer{os.Stderr}
	if f, err := openLogFile(dataRoot); err == nil && f != nil {
		writers = append(writers, f)
	}
	return logging.New(io.MultiWriter(writers...), slog.LevelInfo, redactor)
}

func openLogFile(dataRoot string) (*os.File, error) {
	dir := filepath.Join(dataRoot, "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, "server.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}
