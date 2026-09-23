package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
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
		aiURL = "http://127.0.0.1:8001"
	}
	client, err := recommendation.NewClient(aiURL)
	if err != nil {
		logger.Error("invalid_ai_service_url", "error", err)
		os.Exit(1)
	}
	svc := &service.Service{Store: store.New(), Recommender: client}
	server := &http.Server{Addr: address, Handler: httptransport.New(svc, logger, strings.Split(origins, ",")), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 90 * time.Second, WriteTimeout: 90 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
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
