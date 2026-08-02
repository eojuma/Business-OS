package main

import (
	"log"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/modules/auth"
	"github.com/businessos/backend/internal/modules/business"
	"github.com/businessos/backend/internal/router"
	"github.com/businessos/backend/internal/shared/database"
)

func main() {
	cfg := config.Load()

	db := database.NewPostgres(cfg)
	_ = database.NewRedis(cfg)


	if err := db.AutoMigrate(&business.Business{}, &auth.User{}); err != nil {
		log.Fatalf("failed to run auto-migration: %v", err)
	}

	r := router.New(db, cfg)

	log.Printf("business-os api starting on :%s (env=%s)", cfg.AppPort, cfg.AppEnv)
	if err := r.Run(":" + cfg.AppPort); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}


// 1c672bfd-d7bc-46c3-a77a-c6affadd44d5 - mock business id for testing purposes