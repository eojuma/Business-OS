package main

import (
	"log"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/router"
	"github.com/businessos/backend/internal/shared/database"
)

func main() {
	cfg := config.Load()

	db := database.NewPostgres(cfg)
	_ = database.NewRedis(cfg)

	r := router.New(db, cfg)

	log.Printf("business-os api starting on :%s (env=%s)", cfg.AppPort, cfg.AppEnv)
	if err := r.Run(":" + cfg.AppPort); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
