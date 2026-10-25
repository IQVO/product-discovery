package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func fullEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL": "postgres://x", "OFFER_AVAILABILITY_URL": "http://oa", "PRICING_URL": "http://pr", "PRODUCT_CONTENT_URL": "http://pc",
	}
}

func TestLoadConfigAcceptsAFullEnvironment(t *testing.T) {
	c, err := loadConfig(envOf(fullEnv()))
	if err != nil || c.databaseURL != "postgres://x" || c.offerAvailabilityURL != "http://oa" || c.pricingURL != "http://pr" || c.productContentURL != "http://pc" {
		t.Fatalf("config = %+v %v", c, err)
	}
}

func TestLoadConfigNamesTheMissingVariable(t *testing.T) {
	for _, name := range requiredEnv {
		env := fullEnv()
		env[name] = "  "
		if _, err := loadConfig(envOf(env)); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("without %s: %v", name, err)
		}
	}
}

func TestRunRefusesAnIncompleteEnvironmentBeforeDialling(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := run(context.Background(), envOf(map[string]string{}), logger); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("err = %v", err)
	}
}
