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
		c := config.Config{Env: tc.env, ClusterName: "test", AWSRegion: tc.region, CoordinatorReplicas: 3}
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

func TestValidate_namespaceValid(t *testing.T) {
	for _, ns := range []string{"gossamerdb", "my-ns", "a1", "my-namespace-123"} {
		c := config.Config{Env: config.EnvLocal, ClusterName: "test", Namespace: ns, CoordinatorReplicas: 3}
		if err := c.Validate(); err != nil {
			t.Errorf("namespace %q: unexpected error: %v", ns, err)
		}
	}
}

func TestValidate_namespaceInvalid(t *testing.T) {
	for _, ns := range []string{"MyNS", "my_ns", "-bad", "bad-", "has space", "has/slash"} {
		c := config.Config{Env: config.EnvLocal, ClusterName: "test", Namespace: ns}
		if err := c.Validate(); err == nil {
			t.Errorf("namespace %q: expected error, got nil", ns)
		}
	}
}

func TestValidate_coordinatorReplicas(t *testing.T) {
	base := config.Config{Env: config.EnvLocal, ClusterName: "test"}

	valid := []int{3, 5, 7, 9}
	for _, n := range valid {
		c := base
		c.CoordinatorReplicas = n
		if err := c.Validate(); err != nil {
			t.Errorf("replicas=%d: unexpected error: %v", n, err)
		}
	}

	invalid := []struct {
		n      int
		reason string
	}{
		{0, "zero"},
		{1, "below minimum"},
		{2, "even and below minimum"},
		{4, "even"},
		{6, "even"},
	}
	for _, tc := range invalid {
		c := base
		c.CoordinatorReplicas = tc.n
		if err := c.Validate(); err == nil {
			t.Errorf("replicas=%d (%s): expected error, got nil", tc.n, tc.reason)
		}
	}
}

func TestApplyDefaults_coordinatorReplicas(t *testing.T) {
	c := config.ApplyDefaults(config.Config{})
	if c.CoordinatorReplicas != 3 {
		t.Errorf("default CoordinatorReplicas = %d, want 3", c.CoordinatorReplicas)
	}
	c2 := config.ApplyDefaults(config.Config{CoordinatorReplicas: 5})
	if c2.CoordinatorReplicas != 5 {
		t.Errorf("explicit CoordinatorReplicas overwritten: got %d, want 5", c2.CoordinatorReplicas)
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
