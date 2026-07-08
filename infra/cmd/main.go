// Package main is the Pulumi program entry point for GossamerDB infrastructure.
// It loads stack configuration and dispatches to the appropriate environment
// provisioner. Each provisioner (local, k8s, aws) is implemented in a
// dedicated story; this file owns only the dispatch logic.
package main

import (
	"fmt"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"gossamerdb/infra/pkg/aws"
	"gossamerdb/infra/pkg/config"
	"gossamerdb/infra/pkg/k8s"
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

// deployK8s provisions RBAC (INFRA-4), DataNode (INFRA-5), and PKI (INFRA-8).
func deployK8s(ctx *pulumi.Context, cfg config.Config) error {
	if err := k8s.NewRBAC(ctx, &cfg, nil); err != nil {
		return err
	}
	if err := k8s.NewDataNode(ctx, &cfg, nil); err != nil {
		return err
	}
	return k8s.NewPKI(ctx, &cfg, nil)
}

// deployAWS provisions an EKS cluster on AWS via INFRA-3.
func deployAWS(ctx *pulumi.Context, cfg config.Config) error {
	_, err := aws.NewCluster(ctx, &cfg)
	return err
}
