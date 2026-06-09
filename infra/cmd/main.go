// Package main is the Pulumi program entry point for GossamerDB infrastructure.
// It loads stack configuration and dispatches to the appropriate environment
// provisioner. Each provisioner (local, k8s, aws) is implemented in a
// dedicated story; this file owns only the dispatch logic.
package main

import (
	"fmt"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"gossamerdb/infra/pkg/config"
	"gossamerdb/infra/pkg/local"
)

func main() {
	pulumi.Run(deploy)
}

// deploy is the top-level Pulumi program function. It loads config from the
// active stack and delegates to the correct provisioner based on Config.Env.
// Returns an error if config is invalid or the env value has no registered
// provisioner.
func deploy(ctx *pulumi.Context) error {
	cfg, err := config.Load(ctx)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	switch cfg.Env {
	case config.EnvLocal:
		return deployLocal(ctx, cfg)
	case config.EnvK8s:
		return deployK8s(ctx, cfg)
	case config.EnvAWS:
		return deployAWS(ctx, cfg)
	default:
		// Validate() already rejects unknown envs; this guards against future Env additions
		// that miss a corresponding case here.
		return fmt.Errorf("unhandled env %q", cfg.Env)
	}
}

// deployLocal provisions a kind cluster for local development via INFRA-2.
func deployLocal(ctx *pulumi.Context, cfg config.Config) error {
	return local.NewCluster(ctx, &cfg)
}

// deployK8s is the stub for INFRA-4 (RBAC baseline).
func deployK8s(ctx *pulumi.Context, cfg config.Config) error {
	return ctx.Log.Info(fmt.Sprintf("k8s stack: cluster=%s namespace=%s", cfg.ClusterName, cfg.Namespace), nil)
}

// deployAWS is the stub for INFRA-3 (EKS provisioner).
func deployAWS(ctx *pulumi.Context, cfg config.Config) error {
	return ctx.Log.Info(fmt.Sprintf("aws stack: region=%s cluster=%s", cfg.AWSRegion, cfg.ClusterName), nil)
}
