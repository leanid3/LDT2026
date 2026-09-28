package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoad_FromEnvOnly(t *testing.T) {
	t.Setenv("DATABASE_HOST", "postgres-test")
	t.Setenv("DATABASE_PORT", "5433")
	t.Setenv("SERVER_PORT", "8081")

	cfg, err := Load("does-not-exist.yaml")
	require.NoError(t, err)

	require.Equal(t, "postgres-test", cfg.Database.Host)
	require.Equal(t, 5433, cfg.Database.Port)
	require.Equal(t, 8081, cfg.Server.Port)
	require.Equal(t, "info", cfg.Logger.Level, "env-default should apply when no config file and no env override")
}

func TestLoad_FromExampleYAML(t *testing.T) {
	cfg, err := Load("../../config.example.yaml")
	require.NoError(t, err)

	require.NotEmpty(t, cfg.Auth.JWTSecret)
	require.Equal(t, []string{"1m", "5m", "15m"}, cfg.Rin.RetryDelays)
	require.Equal(t, "localhost:3310", cfg.ClamAV.Address)
}
