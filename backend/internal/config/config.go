// Package config holds application configuration loaded from environment variables.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds all application configuration.
type Config struct {
	AppEnv         string
	AppPort        string
	FrontendURL    string
	AllowedOrigins []string

	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBSSLMode  string

	RedisHost     string
	RedisPort     string
	RedisPassword string

	JWTSecret      string
	JWTExpiryHours int

	// GoogleClientID is the OAuth 2.0 Web client ID used to verify Google
	// Identity Services ID tokens. When empty, /auth/google returns 503.
	GoogleClientID string

	AIProvider string
	AIAPIKey   string
	AIModel    string
	AIBaseURL  string

	// AIOCRModel is the vision-capable model used for receipt OCR. It falls
	// back to AIModel when unset.
	AIOCRModel string
}

// Load loads configuration from environment variables.
func Load() *Config {
	_ = godotenv.Load()
	appEnv := getEnv("APP_ENV", "development")
	redisHost, redisPort, redisPassword := parseRedisURL(getEnv("REDIS_URL", "redis://localhost:6379"))

	return &Config{
		AppEnv:         appEnv,
		AppPort:        getEnv("APP_PORT", "8080"),
		FrontendURL:    getEnv("FRONTEND_URL", "http://localhost:3000"),
		AllowedOrigins: parseOrigins(getEnv("FRONTEND_URL", "http://localhost:3000"), appEnv == "production"),

		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "5432"),
		DBUser:     getEnv("DB_USER", "businessos"),
		DBPassword: getEnv("DB_PASSWORD", "businessos"),
		DBName:     getEnv("DB_NAME", "businessos"),
		DBSSLMode:  getEnv("DB_SSLMODE", "disable"),

		RedisHost:     redisHost,
		RedisPort:     redisPort,
		RedisPassword: redisPassword,

		JWTSecret:      getEnv("JWT_SECRET", "dev_secret_change_me"),
		JWTExpiryHours: getEnvInt("JWT_EXPIRY_HOURS", 24),

		GoogleClientID: getEnv("GOOGLE_CLIENT_ID", ""),

		AIProvider: getEnv("AI_PROVIDER", "gemini"),
		AIAPIKey:   getEnv("AI_API_KEY", ""),
		AIModel:    getEnv("AI_MODEL", "gemini-3.6-flash"),
		AIBaseURL:  getEnv("AI_BASE_URL", "https://generativelanguage.googleapis.com/v1beta/openai"),
		AIOCRModel: getEnv("AI_OCR_MODEL", ""),
	}
}

// IsProduction returns true if the application is running in production mode.
func (c *Config) IsProduction() bool {
	return c.AppEnv == "production"
}

// Validate validates the configuration for production readiness.
func (c *Config) Validate() error {
	if !c.IsProduction() {
		return nil
	}
	if c.JWTSecret == "dev_secret_change_me" || c.JWTSecret == "change_me_in_production" || len(c.JWTSecret) < 32 {
		return fmt.Errorf("JWT_SECRET must be a unique value of at least 32 characters in production")
	}
	if c.DBPassword == "businessos" {
		return fmt.Errorf("DB_PASSWORD must not use the development default in production")
	}
	if c.DBSSLMode == "disable" {
		return fmt.Errorf("DB_SSLMODE must enable TLS in production")
	}
	return nil
}

// parseOrigins splits a comma-separated FRONTEND_URL into an allowlist.
// Outside production it also allows the local development origins so
// `npm run dev` keeps working; production trusts only configured origins.
func parseOrigins(raw string, production bool) []string {
	seen := make(map[string]bool)
	origins := make([]string, 0)
	add := func(origin string) {
		origin = strings.TrimSpace(origin)
		if origin != "" && !seen[origin] {
			seen[origin] = true
			origins = append(origins, origin)
		}
	}

	for _, origin := range strings.Split(raw, ",") {
		add(origin)
	}
	if !production {
		add("http://localhost:3000")
		add("http://127.0.0.1:3000")
	}

	return origins
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func parseRedisURL(raw string) (host, port, password string) {
	u, err := url.Parse(raw)
	if err != nil {
		return "localhost", "6379", ""
	}
	host = u.Hostname()
	port = u.Port()
	if port == "" {
		port = "6379"
	}
	if u.User != nil {
		password, _ = u.User.Password()
	}
	return
}
