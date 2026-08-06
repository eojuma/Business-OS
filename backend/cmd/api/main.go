package main

import (
	"log"

	"github.com/businessos/backend/internal/config"
	"github.com/businessos/backend/internal/modules/auth"
	"github.com/businessos/backend/internal/modules/business"
	"github.com/businessos/backend/internal/modules/customers"
	"github.com/businessos/backend/internal/modules/inventory"
	"github.com/businessos/backend/internal/modules/products"
	"github.com/businessos/backend/internal/modules/sales"
	"github.com/businessos/backend/internal/router"
	"github.com/businessos/backend/internal/shared/database"
)

func main() {
	cfg := config.Load()

	db := database.NewPostgres(cfg)
	_ = database.NewRedis(cfg)

	if err := db.AutoMigrate(
		&business.Business{},
		&auth.User{},
		&products.Product{},
		&inventory.StockLevel{},
		&inventory.StockMovement{},
		&sales.Sale{},
		&sales.SaleLineItem{},
		&customers.Customer{},
	); err != nil {
		log.Fatalf("failed to run auto-migration: %v", err)
	}

	r := router.New(db, cfg)

	log.Printf("business-os api starting on :%s (env=%s)", cfg.AppPort, cfg.AppEnv)
	if err := r.Run(":" + cfg.AppPort); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}