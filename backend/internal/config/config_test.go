package config

import (
	"reflect"
	"testing"
)

func TestParseOriginsAllowsLocalOriginsOutsideProduction(t *testing.T) {
	got := parseOrigins("https://app.example.com, https://admin.example.com", false)
	want := []string{
		"https://app.example.com",
		"https://admin.example.com",
		"http://localhost:3000",
		"http://127.0.0.1:3000",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("origins = %v, want %v", got, want)
	}
}

func TestParseOriginsRestrictsToConfiguredOriginsInProduction(t *testing.T) {
	got := parseOrigins("https://app.example.com", true)
	want := []string{"https://app.example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("origins = %v, want %v", got, want)
	}
}

func TestParseOriginsDeduplicatesAndTrims(t *testing.T) {
	got := parseOrigins("https://app.example.com, https://app.example.com ,", true)
	want := []string{"https://app.example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("origins = %v, want %v", got, want)
	}
}

func TestLoadNormalizesAppEnv(t *testing.T) {
	t.Setenv("APP_ENV", " Production ")
	cfg := Load()
	if cfg.AppEnv != "production" {
		t.Fatalf("APP_ENV = %q, want %q", cfg.AppEnv, "production")
	}
	if !cfg.IsProduction() {
		t.Fatal("expected production mode")
	}
}

func TestIsProductionNormalizesAppEnv(t *testing.T) {
	for _, env := range []string{"production", "Production", " PRODUCTION "} {
		if !(&Config{AppEnv: env}).IsProduction() {
			t.Fatalf("APP_ENV %q was not treated as production", env)
		}
	}
}

func TestValidateRejectsUnknownAppEnv(t *testing.T) {
	for _, env := range []string{"", "prod", "Producton", "live"} {
		if err := (&Config{AppEnv: env}).Validate(); err == nil {
			t.Fatalf("APP_ENV %q was accepted, want a validation error", env)
		}
	}
}

func TestValidateAllowsKnownNonProductionEnvs(t *testing.T) {
	for _, env := range []string{"development", "test", "staging", "Staging"} {
		if err := (&Config{AppEnv: env}).Validate(); err != nil {
			t.Fatalf("APP_ENV %q rejected: %v", env, err)
		}
	}
}
