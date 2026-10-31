package database

import (
	"fmt"
	"log"

	"github.com/businessos/backend/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// NewPostgres creates a new PostgreSQL database connection using the provided configuration.
// Configures connection pool settings and log level based on environment.
func NewPostgres(cfg *config.Config) *gorm.DB {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBSSLMode,
	)

	logLevel := logger.Warn
	if !cfg.IsProduction() {
		logLevel = logger.Info
	}

	return openPostgres(dsn, logLevel)
}

// NewPostgresFromURL creates a new PostgreSQL database connection using a
// PostgreSQL URL connection string.
func NewPostgresFromURL(databaseURL string) *gorm.DB {
	return openPostgres(databaseURL, logger.Warn)
}

func openPostgres(dsn string, logLevel logger.LogLevel) *gorm.DB {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger:         logger.Default.LogMode(logLevel),
		TranslateError: true,
	})
	if err != nil {
		log.Fatalf("failed to connect to postgres: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("failed to get underlying sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(10)

	return db
}
