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

// TestParseCSVTrimsAndDropsEmptyValues documents its scenario and asserts the expected audit behaviour.
func TestParseCSVTrimsAndDropsEmptyValues(t *testing.T) {
	got := parseCSV(" 10.0.0.0/8, ,192.168.1.0/24,")
	want := []string{"10.0.0.0/8", "192.168.1.0/24"}
	if len(got) != len(want) {
		t.Fatalf("parseCSV length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parseCSV[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
