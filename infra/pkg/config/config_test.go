package config_test

import (
	"testing"

	"gossamerdb/infra/pkg/config"
)

func TestValidate_knownEnvs(t *testing.T) {
	envs := []config.Env{config.EnvLocal, config.EnvK8s, config.EnvAWS}
	for _, env := range envs {
		c := config.Config{Env: env, ClusterName: "test"}
		if err := c.Validate(); err != nil {
			t.Fatalf("env %q: unexpected error: %v", env, err)
		}
	}
}

func TestValidate_unknownEnv(t *testing.T) {
	c := config.Config{Env: "prod", ClusterName: "test"}
	err := c.Validate()
	if err == nil {
		t.Fatal("expected error for unknown env, got nil")
	}
}

func TestValidate_emptyClusterName(t *testing.T) {
	c := config.Config{Env: config.EnvLocal, ClusterName: ""}
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for empty ClusterName, got nil")
	}
}
