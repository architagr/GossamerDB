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
	// clusterRoleCoordinator grants the coordinator SA cluster-wide peer-discovery
	// access: nodes (topology), endpoints (service backends), pods (gossip peers).
	clusterRoleCoordinator = "gossamerdb:coordinator-discovery"
	// clusterRoleDatanode grants the datanode SA peer-discovery access limited to
	// endpoints and pods — it has no need to observe node topology.
	clusterRoleDatanode = "gossamerdb:datanode-discovery"
	// roleSecretReader grants namespace-scoped read on secrets (PKI source, HLD §5.7).
	roleSecretReader = "gossamerdb:secret-reader"
	saCoordinator    = "coordinator"
	saDatanode       = "datanode"
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

// NewRBAC creates the GossamerDB namespace, service accounts, and RBAC
// bindings required by coordinator and data-node workloads. All resources are
// pure Kubernetes objects (no cloud-specific annotations) and work against both
// local kind clusters and AWS EKS.
//
// Least-privilege design:
//   - coordinator SA: ClusterRole with nodes+endpoints+pods (topology + peer discovery)
//   - datanode SA: ClusterRole with endpoints+pods only (peer discovery, no node topology)
//   - both SAs: namespace-scoped Role with secret get (PKI source)
//
// provider may be nil under Pulumi's mock test harness.
func NewRBAC(ctx *pulumi.Context, cfg *config.Config, provider *kubernetes.Provider) error {
	ns := namespaceName(cfg)

	nsRes, err := corev1.NewNamespace(ctx, ns, &corev1.NamespaceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String(ns),
			Labels: pulumi.StringMap{
				"app.kubernetes.io/managed-by": pulumi.String("pulumi"),
			},
		},
	}, resOpts(provider)...)
	if err != nil {
		return fmt.Errorf("namespace: %w", err)
	}

	coordinatorSA, err := corev1.NewServiceAccount(ctx, saCoordinator, &corev1.ServiceAccountArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(saCoordinator),
			Namespace: pulumi.String(ns),
		},
		// Opt out of automatic token mounting; workloads that need the token must
		// request it explicitly via a projected volume.
		AutomountServiceAccountToken: pulumi.BoolPtr(false),
	}, resOpts(provider, pulumi.Parent(nsRes))...)
	if err != nil {
		return fmt.Errorf("service account %s: %w", saCoordinator, err)
	}

	datanodeSA, err := corev1.NewServiceAccount(ctx, saDatanode, &corev1.ServiceAccountArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(saDatanode),
			Namespace: pulumi.String(ns),
		},
		AutomountServiceAccountToken: pulumi.BoolPtr(false),
	}, resOpts(provider, pulumi.Parent(nsRes))...)
	if err != nil {
		return fmt.Errorf("service account %s: %w", saDatanode, err)
	}

	// Coordinator ClusterRole: nodes (topology), endpoints, pods (gossip peers).
	coordinatorCR, err := rbacv1.NewClusterRole(ctx, clusterRoleCoordinator, &rbacv1.ClusterRoleArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String(clusterRoleCoordinator),
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
	}, resOpts(provider)...)
	if err != nil {
		return fmt.Errorf("cluster role %s: %w", clusterRoleCoordinator, err)
	}

	if _, err := rbacv1.NewClusterRoleBinding(ctx, clusterRoleCoordinator, &rbacv1.ClusterRoleBindingArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String(clusterRoleCoordinator),
		},
		RoleRef: rbacv1.RoleRefArgs{
			ApiGroup: pulumi.StringPtr("rbac.authorization.k8s.io"),
			Kind:     pulumi.String("ClusterRole"),
			Name:     pulumi.String(clusterRoleCoordinator),
		},
		Subjects: rbacv1.SubjectArray{
			rbacv1.SubjectArgs{
				Kind:      pulumi.String("ServiceAccount"),
				Name:      coordinatorSA.Metadata.Name().Elem(),
				Namespace: pulumi.String(ns),
			},
		},
	}, resOpts(provider, pulumi.Parent(coordinatorCR))...); err != nil {
		return fmt.Errorf("cluster role binding %s: %w", clusterRoleCoordinator, err)
	}

	// Datanode ClusterRole: endpoints + pods only; no node topology visibility.
	datanodeCR, err := rbacv1.NewClusterRole(ctx, clusterRoleDatanode, &rbacv1.ClusterRoleArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String(clusterRoleDatanode),
		},
		Rules: rbacv1.PolicyRuleArray{
			rbacv1.PolicyRuleArgs{
				ApiGroups: pulumi.StringArray{pulumi.String("")},
				Resources: pulumi.StringArray{
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
	}, resOpts(provider)...)
	if err != nil {
		return fmt.Errorf("cluster role %s: %w", clusterRoleDatanode, err)
	}

	if _, err := rbacv1.NewClusterRoleBinding(ctx, clusterRoleDatanode, &rbacv1.ClusterRoleBindingArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String(clusterRoleDatanode),
		},
		RoleRef: rbacv1.RoleRefArgs{
			ApiGroup: pulumi.StringPtr("rbac.authorization.k8s.io"),
			Kind:     pulumi.String("ClusterRole"),
			Name:     pulumi.String(clusterRoleDatanode),
		},
		Subjects: rbacv1.SubjectArray{
			rbacv1.SubjectArgs{
				Kind:      pulumi.String("ServiceAccount"),
				Name:      datanodeSA.Metadata.Name().Elem(),
				Namespace: pulumi.String(ns),
			},
		},
	}, resOpts(provider, pulumi.Parent(datanodeCR))...); err != nil {
		return fmt.Errorf("cluster role binding %s: %w", clusterRoleDatanode, err)
	}

	// Role: namespace-scoped secret read for PKI source (HLD §5.7).
	// TODO INFRA-8: restrict to specific secret resourceNames once cert-manager
	// secret names are known (currently unset = all secrets in namespace).
	secretRole, err := rbacv1.NewRole(ctx, roleSecretReader, &rbacv1.RoleArgs{
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
	}, resOpts(provider, pulumi.Parent(nsRes))...)
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
		Subjects: rbacv1.SubjectArray{
			rbacv1.SubjectArgs{
				Kind:      pulumi.String("ServiceAccount"),
				Name:      coordinatorSA.Metadata.Name().Elem(),
				Namespace: pulumi.String(ns),
			},
			rbacv1.SubjectArgs{
				Kind:      pulumi.String("ServiceAccount"),
				Name:      datanodeSA.Metadata.Name().Elem(),
				Namespace: pulumi.String(ns),
			},
		},
	}, resOpts(provider, pulumi.Parent(secretRole))...); err != nil {
		return fmt.Errorf("role binding: %w", err)
	}

	return nil
}
