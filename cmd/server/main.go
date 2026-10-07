package main

import (
	"context"
	"errors"
	"ghostview/internal/cache"
	"ghostview/internal/config"
	"ghostview/internal/downloader"
	"ghostview/internal/handler"
	"ghostview/internal/model"
	"ghostview/internal/provider"
	"ghostview/internal/provider/facebook"
	"ghostview/internal/provider/instagram"
	"ghostview/internal/provider/mock"
	"ghostview/internal/provider/tiktok"
	"ghostview/internal/service"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration invalid", "error", err)
		os.Exit(1)
	}
	providers := []provider.SocialProvider{tiktok.NewWithBrowser(cfg.TTBrowserEnabled), instagram.NewWithSession(cfg.IGSessionID), facebook.New()}
	if cfg.Mode == "mock" {
		providers = []provider.SocialProvider{mock.New(model.TikTok), mock.New(model.Instagram), mock.New(model.Facebook)}
	}
	svc := service.New(providers, cache.NewMemory(cfg.MaxCacheEntries), cfg.ProviderTimeout)
	dl := downloader.New(cfg.DownloadTimeout, cfg.DownloadMaxBytes)
	defer dl.Close()
	server := &http.Server{Addr: cfg.Addr, Handler: handler.New(svc, dl, cfg, logger), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: cfg.ProviderTimeout + cfg.DownloadTimeout + 5*time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() {
		logger.Info("GhostView listening", "address", cfg.Addr, "provider_mode", cfg.Mode)
		done <- server.ListenAndServe()
	}()
	select {
	case err = <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed")
			os.Exit(1)
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err = server.Shutdown(shutdown); err != nil {
			logger.Error("graceful shutdown timed out")
			_ = server.Close()
		}
	}
}
