package main

import (
	"log"
	"os"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/router"
	"github.com/businessos/backend/internal/shared/database"
	"github.com/businessos/backend/internal/shared/migrations"
)

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
			log.Fatalf("unknown migration command %q (expected up or down)", os.Args[2])
		}
		return
	}

	if err := migrations.Up(db); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	r := router.New(db, cfg)

	log.Printf("business-os api starting on :%s (env=%s)", cfg.AppPort, cfg.AppEnv)
	if err := r.Run(":" + cfg.AppPort); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
