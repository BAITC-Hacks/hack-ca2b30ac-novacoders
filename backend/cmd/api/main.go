package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/recommendation"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/service"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/store"
	httptransport "github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/transport/http"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	address := os.Getenv("HTTP_ADDR")
	if address == "" {
		address = "127.0.0.1:8080"
	}
	origins := os.Getenv("CORS_ORIGINS")
	if origins == "" {
		origins = "http://localhost:5173,http://127.0.0.1:5173,http://localhost:3000,http://127.0.0.1:3000"
	}
	aiURL := os.Getenv("AI_SERVICE_URL")
	if aiURL == "" {
		logger.Error("missing_ai_service_url", "message", "Укажите AI_SERVICE_URL, например http://127.0.0.1:8001")
		os.Exit(1)
	}
	deadline := recommendation.DefaultServiceDeadline
	if value := os.Getenv("AI_REQUEST_TIMEOUT_SECONDS"); value != "" {
		var err error
		deadline, err = time.ParseDuration(value + "s")
		if err != nil {
			logger.Error("invalid_ai_deadline", "message", "AI_REQUEST_TIMEOUT_SECONDS должен быть числом секунд")
			os.Exit(1)
		}
	}
	client, err := recommendation.NewConfiguredClient(recommendation.ClientConfig{BaseURL: aiURL, Token: os.Getenv("AI_SERVICE_TOKEN"), ServiceDeadline: deadline})
	if err != nil {
		logger.Error("invalid_ai_service_config", "error", err)
		os.Exit(1)
	}
	svc := &service.Service{Store: store.New(), Recommender: client}
	cwd, err := os.Getwd()
	if err != nil {
		logger.Error("working_directory_unavailable", "error", err)
		os.Exit(1)
	}
	dataRoot := localDataDirectory(cwd, os.Getenv("DATA_DEMO_DIR"))
	server := &http.Server{
		Addr:              address,
		Handler:           httptransport.New(svc, logger, strings.Split(origins, ","), httptransport.WithLocalDatasets(dataRoot)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       90 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	failed := make(chan error, 1)
	go func() { logger.Info("server_started", "address", address); failed <- server.ListenAndServe() }()
	select {
	case err := <-failed:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server_failed", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			logger.Error("shutdown_failed")
			_ = server.Close()
		}
		logger.Info("server_stopped")
	}
}

// Resolve the default against the backend module when launched from the repo
// root. Explicit DATA_DEMO_DIR values remain relative to the working directory.
func localDataDirectory(cwd, configured string) string {
	if configured != "" {
		if filepath.IsAbs(configured) {
			return filepath.Clean(configured)
		}
		return filepath.Join(cwd, configured)
	}
	if info, err := os.Stat(filepath.Join(cwd, "backend", "go.mod")); err == nil && info.Mode().IsRegular() {
		return filepath.Join(cwd, "backend", "data", "demo")
	}
	return filepath.Join(cwd, "data", "demo")
}
