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
