package config

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	pulumiconfig "github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

// Load reads stack configuration values from the Pulumi context and returns a
// populated, validated Config. cfg.Require panics with a clear message if "env"
// or "clusterName" are absent from the stack's YAML config — this is standard
// Pulumi SDK behavior. All other keys are optional and fall back to safe
// defaults via ApplyDefaults. Returns a non-nil error only when Validate finds
// the resulting Config ill-formed.
func Load(ctx *pulumi.Context) (Config, error) {
	cfg := pulumiconfig.New(ctx, "")

	c := Config{
		Env:         Env(cfg.Require("env")),
		ClusterName: cfg.Require("clusterName"),
		K8sVersion:  cfg.Get("k8sVersion"),
		NodeCount:   cfg.GetInt("nodeCount"),
		AWSRegion:   cfg.Get("awsRegion"),
		Namespace:               cfg.Get("namespace"),
		CoordinatorImage:        cfg.Get("coordinatorImage"),
		CoordinatorStorageClass: cfg.Get("coordinatorStorageClass"),
	}

	c = ApplyDefaults(c)
	return c, c.Validate()
}

// ApplyDefaults returns a copy of c with zero-value optional fields filled in
// with safe defaults. It is a pure function: it does not perform I/O and does
// not modify c in place. Callers that construct a Config outside of Load (e.g.
// in tests) should call ApplyDefaults before Validate.
func ApplyDefaults(c Config) Config {
	if c.K8sVersion == "" {
		c.K8sVersion = "1.29"
	}
	if c.NodeCount == 0 {
		c.NodeCount = 1
	}
	if c.Namespace == "" {
		c.Namespace = "gossamerdb"
	}
	if c.CoordinatorStorageClass == "" {
		if c.Env == EnvAWS {
			c.CoordinatorStorageClass = "gp3"
		} else {
			c.CoordinatorStorageClass = "standard"
		}
	}
	return c
}
