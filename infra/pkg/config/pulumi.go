package config

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	pulumiconfig "github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

// Load reads stack configuration values from the Pulumi context and returns a
// populated, validated Config. It panics (via cfg.Require) if "env" or
// "clusterName" are absent from the stack's YAML config; all other keys are
// optional and fall back to safe defaults. Returns a non-nil error only when
// Validate() finds the resulting Config ill-formed.
func Load(ctx *pulumi.Context) (Config, error) {
	cfg := pulumiconfig.New(ctx, "")

	c := Config{
		Env:         Env(cfg.Require("env")),
		ClusterName: cfg.Require("clusterName"),
		K8sVersion:  cfg.Get("k8sVersion"),
		NodeCount:   cfg.GetInt("nodeCount"),
		AWSRegion:   cfg.Get("awsRegion"),
		Namespace:   cfg.Get("namespace"),
	}

	if c.K8sVersion == "" {
		c.K8sVersion = "1.29"
	}
	if c.NodeCount == 0 {
		c.NodeCount = 1
	}
	if c.Namespace == "" {
		c.Namespace = "gossamerdb"
	}

	return c, c.Validate()
}
