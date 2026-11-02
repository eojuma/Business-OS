package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/modules/notifications"
	"github.com/businessos/backend/internal/router"
	"github.com/businessos/backend/internal/shared/database"
	"github.com/businessos/backend/internal/shared/migrations"
)

// main is the entry point for the business-os API server.
// It loads configuration, connects to the database, runs migrations,
// starts background schedulers, and launches the HTTP router.
func main() {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	db := database.NewPostgres(cfg)
	_ = database.NewRedis(cfg)

	if len(os.Args) >= 3 && os.Args[1] == "migrate" {
		switch os.Args[2] {
		case "up":
			if err := migrations.Up(db); err != nil {
				log.Fatalf("failed to run migrations: %v", err)
			}
		case "down":
			if err := migrations.Down(db); err != nil {
				log.Fatalf("failed to roll back migration: %v", err)
			}
		default:
			log.Fatalf("unknown migration command %q (expected up or down)", os.Args[2]) //nolint:gosec
		}
		return
	}

	if err := migrations.Up(db); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	// Background alerts: low stock and credit limits. Runs once at startup
	// and then every 15 minutes.
	notifications.StartScheduler(db, 15*time.Minute)

	engine, waitAuthBackground := router.New(db, cfg)

	server := &http.Server{
		Addr:              ":" + cfg.AppPort,
		Handler:           engine,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("business-os api starting on :%s (env=%s)", cfg.AppPort, cfg.AppEnv)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Printf("business-os api shutting down")

	// Drain HTTP handlers first so no request can enqueue more background work,
	// then wait for the pending verification sends to finish.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown: %v", err)
	}
	waitAuthBackground(shutdownCtx)
	log.Printf("business-os api stopped")
}
