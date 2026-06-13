// Package k8s provisions Kubernetes RBAC baseline resources for GossamerDB.
// It is responsible for creating the namespace, service accounts, roles, and
// bindings needed by the coordinator and data-node workloads. It is NOT
// responsible for provisioning the cluster itself or managing kubeconfig files.
// Key entry point: [NewRBAC].
package k8s

import (
	"fmt"

	"gossamerdb/infra/pkg/config"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	rbacv1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/rbac/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const (
	clusterRoleNodeDiscovery = "gossamerdb:node-discovery"
	roleSecretReader         = "gossamerdb:secret-reader"
	saCoordinator            = "coordinator"
	saDatanode               = "datanode"
)

// namespaceName returns cfg.Namespace when set; otherwise "gossamerdb".
func namespaceName(cfg *config.Config) string {
	if cfg.Namespace != "" {
		return cfg.Namespace
	}
	return "gossamerdb"
}

// opts builds a ResourceOption slice that includes the provider (if non-nil)
// plus any extra options. Allocating a fresh slice each call prevents aliasing
// bugs when callers append extras to the result.
func opts(provider *kubernetes.Provider, extra ...pulumi.ResourceOption) []pulumi.ResourceOption {
	var out []pulumi.ResourceOption
	if provider != nil {
		out = append(out, pulumi.Provider(provider))
	}
	return append(out, extra...)
}

// NewRBAC creates the GossamerDB namespace, service accounts, and RBAC
// bindings required by coordinator and data-node workloads. Resources are pure
// Kubernetes objects (no cloud-specific annotations), so they work against both
// local kind clusters and AWS EKS.
//
// provider may be nil when running under Pulumi's mock test harness.
func NewRBAC(ctx *pulumi.Context, cfg *config.Config, provider *kubernetes.Provider) error {
	ns := namespaceName(cfg)

	nsRes, err := corev1.NewNamespace(ctx, ns, &corev1.NamespaceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String(ns),
			Labels: pulumi.StringMap{
				"app.kubernetes.io/managed-by": pulumi.String("pulumi"),
			},
		},
	}, opts(provider)...)
	if err != nil {
		return fmt.Errorf("namespace: %w", err)
	}

	for _, name := range []string{saCoordinator, saDatanode} {
		if _, err := corev1.NewServiceAccount(ctx, name, &corev1.ServiceAccountArgs{
			Metadata: &metav1.ObjectMetaArgs{
				Name:      pulumi.String(name),
				Namespace: pulumi.String(ns),
			},
		}, opts(provider, pulumi.Parent(nsRes))...); err != nil {
			return fmt.Errorf("service account %s: %w", name, err)
		}
	}

	clusterRole, err := rbacv1.NewClusterRole(ctx, clusterRoleNodeDiscovery, &rbacv1.ClusterRoleArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String(clusterRoleNodeDiscovery),
		},
		Rules: rbacv1.PolicyRuleArray{
			rbacv1.PolicyRuleArgs{
				ApiGroups: pulumi.StringArray{pulumi.String("")},
				Resources: pulumi.StringArray{
					pulumi.String("nodes"),
					pulumi.String("endpoints"),
					pulumi.String("pods"),
				},
				Verbs: pulumi.StringArray{
					pulumi.String("get"),
					pulumi.String("list"),
					pulumi.String("watch"),
				},
			},
		},
	}, opts(provider)...)
	if err != nil {
		return fmt.Errorf("cluster role: %w", err)
	}

	subjects := buildSubjects([]string{saCoordinator, saDatanode}, ns)

	if _, err := rbacv1.NewClusterRoleBinding(ctx, clusterRoleNodeDiscovery, &rbacv1.ClusterRoleBindingArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String(clusterRoleNodeDiscovery),
		},
		RoleRef: rbacv1.RoleRefArgs{
			ApiGroup: pulumi.StringPtr("rbac.authorization.k8s.io"),
			Kind:     pulumi.String("ClusterRole"),
			Name:     pulumi.String(clusterRoleNodeDiscovery),
		},
		Subjects: subjects,
	}, opts(provider, pulumi.Parent(clusterRole))...); err != nil {
		return fmt.Errorf("cluster role binding: %w", err)
	}

	role, err := rbacv1.NewRole(ctx, roleSecretReader, &rbacv1.RoleArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(roleSecretReader),
			Namespace: pulumi.String(ns),
		},
		Rules: rbacv1.PolicyRuleArray{
			rbacv1.PolicyRuleArgs{
				ApiGroups: pulumi.StringArray{pulumi.String("")},
				Resources: pulumi.StringArray{pulumi.String("secrets")},
				Verbs:     pulumi.StringArray{pulumi.String("get")},
			},
		},
	}, opts(provider, pulumi.Parent(nsRes))...)
	if err != nil {
		return fmt.Errorf("role: %w", err)
	}

	if _, err := rbacv1.NewRoleBinding(ctx, roleSecretReader, &rbacv1.RoleBindingArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(roleSecretReader),
			Namespace: pulumi.String(ns),
		},
		RoleRef: rbacv1.RoleRefArgs{
			ApiGroup: pulumi.StringPtr("rbac.authorization.k8s.io"),
			Kind:     pulumi.String("Role"),
			Name:     pulumi.String(roleSecretReader),
		},
		Subjects: subjects,
	}, opts(provider, pulumi.Parent(role))...); err != nil {
		return fmt.Errorf("role binding: %w", err)
	}

	return nil
}

// buildSubjects returns a SubjectArray for the given service account names.
func buildSubjects(names []string, namespace string) rbacv1.SubjectArray {
	out := make(rbacv1.SubjectArray, 0, len(names))
	for _, name := range names {
		out = append(out, rbacv1.SubjectArgs{
			Kind:      pulumi.String("ServiceAccount"),
			Name:      pulumi.String(name),
			Namespace: pulumi.StringPtr(namespace),
		})
	}
	return out
}
