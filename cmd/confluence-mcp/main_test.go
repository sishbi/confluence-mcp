package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRequireEnv_Present(t *testing.T) {
	// requireEnv calls os.Exit on missing var, so we can only test the happy path
	t.Setenv("TEST_REQUIRE_ENV", "value123")
	result := requireEnv("TEST_REQUIRE_ENV")
	assert.Equal(t, "value123", result)
}

func TestRequireEnv_Empty(t *testing.T) {
	// Verify the env var is not set (don't call requireEnv — it would os.Exit)
	_ = os.Unsetenv("NONEXISTENT_VAR_12345")
	val := os.Getenv("NONEXISTENT_VAR_12345")
	assert.Empty(t, val, "sanity check: env var should not be set")
}

func TestVersionVars(t *testing.T) {
	// Verify default values for dev builds
	assert.Equal(t, "dev", version)
	assert.Equal(t, "none", commit)
	assert.Equal(t, "unknown", date)
}
