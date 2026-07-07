package k8s

import (
	"fmt"

	"gossamerdb/infra/pkg/config"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const (
	svcClientGRPC = "gossamerdb-client-grpc"
	svcClientREST = "gossamerdb-client-rest"
	svcAdminGRPC  = "gossamerdb-admin-grpc"

	portClientGRPC = 9090
	portClientREST = 8080
	portAdminGRPC  = 9091

	nlbTypeAnnotation   = "service.beta.kubernetes.io/aws-load-balancer-type"
	nlbSchemeAnnotation = "service.beta.kubernetes.io/aws-load-balancer-scheme"
)

// svcSpec describes one Service to be created.
type svcSpec struct {
	name     string
	port     int
	role     string // pod selector value for the "role" label
}

// NewServices creates the three L4 Services for client and admin traffic:
//   - gossamerdb-client-grpc (port 9090) → datanode pods
//   - gossamerdb-client-rest (port 8080) → datanode pods
//   - gossamerdb-admin-grpc  (port 9091) → coordinator pods
//
// On AWS: type=LoadBalancer with NLB annotations.
// On local/k8s: type=ClusterIP, no cloud annotations.
// provider may be nil under Pulumi's mock test harness.
func NewServices(ctx *pulumi.Context, cfg *config.Config, provider *kubernetes.Provider) error {
	ns := namespaceName(cfg)

	specs := []svcSpec{
		{name: svcClientGRPC, port: portClientGRPC, role: "datanode"},
		{name: svcClientREST, port: portClientREST, role: "datanode"},
		{name: svcAdminGRPC, port: portAdminGRPC, role: "coordinator"},
	}

	for _, s := range specs {
		if err := newService(ctx, cfg, provider, ns, s); err != nil {
			return err
		}
	}
	return nil
}

func newService(
	ctx *pulumi.Context,
	cfg *config.Config,
	provider *kubernetes.Provider,
	ns string,
	s svcSpec,
) error {
	var svcType string
	var annotations pulumi.StringMap

	if cfg.Env == config.EnvAWS {
		svcType = "LoadBalancer"
		annotations = pulumi.StringMap{
			nlbTypeAnnotation:   pulumi.String("nlb"),
			nlbSchemeAnnotation: pulumi.String("internet-facing"),
		}
	} else {
		svcType = "ClusterIP"
	}

	meta := &metav1.ObjectMetaArgs{
		Name:      pulumi.String(s.name),
		Namespace: pulumi.String(ns),
	}
	if annotations != nil {
		meta.Annotations = annotations
	}

	_, err := corev1.NewService(ctx, s.name, &corev1.ServiceArgs{
		Metadata: meta,
		Spec: &corev1.ServiceSpecArgs{
			Type: pulumi.String(svcType),
			Selector: pulumi.StringMap{
				"app":  pulumi.String("gossamerdb"),
				"role": pulumi.String(s.role),
			},
			Ports: corev1.ServicePortArray{
				corev1.ServicePortArgs{
					Port:     pulumi.Int(s.port),
					Protocol: pulumi.String("TCP"),
				},
			},
		},
	}, resOpts(provider)...)
	if err != nil {
		return fmt.Errorf("service %s: %w", s.name, err)
	}
	return nil
}
