package config_test

import (
	"testing"

	"gossamerdb/infra/pkg/config"
)

func TestValidate_knownEnvs(t *testing.T) {
	cases := []struct {
		env    config.Env
		region string
	}{
		{config.EnvLocal, ""},
		{config.EnvK8s, ""},
		{config.EnvAWS, "us-east-1"},
	}
	for _, tc := range cases {
		c := config.Config{Env: tc.env, ClusterName: "test", AWSRegion: tc.region}
		if err := c.Validate(); err != nil {
			t.Errorf("env %q: unexpected error: %v", tc.env, err)
		}
	}
}

func TestValidate_unknownEnv(t *testing.T) {
	c := config.Config{Env: "prod", ClusterName: "test"}
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for unknown env, got nil")
	}
}

func TestValidate_emptyEnv(t *testing.T) {
	c := config.Config{ClusterName: "test"}
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for empty env, got nil")
	}
}

func TestValidate_emptyClusterName(t *testing.T) {
	c := config.Config{Env: config.EnvLocal}
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for empty ClusterName, got nil")
	}
}

func TestValidate_awsRequiresRegion(t *testing.T) {
	c := config.Config{Env: config.EnvAWS, ClusterName: "test", AWSRegion: ""}
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for EnvAWS with empty AWSRegion, got nil")
	}
}

func TestApplyDefaults(t *testing.T) {
	cases := []struct {
		name  string
		input config.Config
		check func(t *testing.T, c config.Config)
	}{
		{
			name:  "defaults applied when zero",
			input: config.Config{},
			check: func(t *testing.T, c config.Config) {
				if c.K8sVersion != "1.29" {
					t.Errorf("K8sVersion: got %q, want %q", c.K8sVersion, "1.29")
				}
				if c.NodeCount != 1 {
					t.Errorf("NodeCount: got %d, want 1", c.NodeCount)
				}
				if c.Namespace != "gossamerdb" {
					t.Errorf("Namespace: got %q, want %q", c.Namespace, "gossamerdb")
				}
			},
		},
		{
			name:  "explicit values not overwritten",
			input: config.Config{K8sVersion: "1.30", NodeCount: 3, Namespace: "custom"},
			check: func(t *testing.T, c config.Config) {
				if c.K8sVersion != "1.30" {
					t.Errorf("K8sVersion: got %q, want %q", c.K8sVersion, "1.30")
				}
				if c.NodeCount != 3 {
					t.Errorf("NodeCount: got %d, want 3", c.NodeCount)
				}
				if c.Namespace != "custom" {
					t.Errorf("Namespace: got %q, want %q", c.Namespace, "custom")
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.check(t, config.ApplyDefaults(tc.input))
		})
	}
}
