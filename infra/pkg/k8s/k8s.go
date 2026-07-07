// Package k8s provisions Kubernetes resources for GossamerDB workloads.
// Shared constants and helpers used across rbac.go and datanode.go.
package k8s

import (
	"gossamerdb/infra/pkg/config"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Service account names referenced by both RBAC bindings and pod specs.
const (
	saCoordinator = "coordinator"
	saDatanode    = "datanode"
)

// namespaceName returns cfg.Namespace when set; otherwise "gossamerdb".
func namespaceName(cfg *config.Config) string {
	if cfg.Namespace != "" {
		return cfg.Namespace
	}
	return "gossamerdb"
}

// resOpts builds a ResourceOption slice with the provider (when non-nil) plus
// any extra options. A new slice is allocated each call to prevent aliasing.
func resOpts(provider *kubernetes.Provider, extra ...pulumi.ResourceOption) []pulumi.ResourceOption {
	var out []pulumi.ResourceOption
	if provider != nil {
		out = append(out, pulumi.Provider(provider))
	}
	return append(out, extra...)
}
