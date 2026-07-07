// Package k8s provisions Kubernetes workload resources for GossamerDB.
// datanode.go creates the DataNode StatefulSet, headless Service, and
// PodDisruptionBudget. Key entry point: [NewDataNode].
package k8s

import (
	"fmt"

	"gossamerdb/infra/pkg/config"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	appsv1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apps/v1"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	policyv1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/policy/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const (
	datanodeStsName        = "gossamerdb-datanode"
	datanodeHeadlessSvcName = "gossamerdb-datanode-headless"
	datanodePDBName        = "gossamerdb-datanode"
	datanodeContainerPort  = 8080
	datanodePDBMinAvail    = 3
)

// NewDataNode creates the DataNode StatefulSet, headless Service, and
// PodDisruptionBudget. All resources are pure Kubernetes objects and work
// against both local kind clusters and AWS EKS.
//
// cfg.DataNodeImage must be non-empty; cfg.NodeCount sets replica count.
// provider may be nil under Pulumi's mock test harness.
func NewDataNode(ctx *pulumi.Context, cfg *config.Config, provider *kubernetes.Provider) error {
	if cfg.DataNodeImage == "" {
		return fmt.Errorf("config.DataNodeImage must not be empty")
	}

	ns := namespaceName(cfg)

	podLabels := pulumi.StringMap{
		"app":     pulumi.String("gossamerdb"),
		"role":    pulumi.String("datanode"),
		"cluster": pulumi.String(cfg.ClusterName),
	}

	svc, err := corev1.NewService(ctx, datanodeHeadlessSvcName, &corev1.ServiceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(datanodeHeadlessSvcName),
			Namespace: pulumi.String(ns),
		},
		Spec: &corev1.ServiceSpecArgs{
			ClusterIP: pulumi.String("None"),
			Selector:  podLabels,
			Ports: corev1.ServicePortArray{
				corev1.ServicePortArgs{
					Name:     pulumi.String("gossip"),
					Port:     pulumi.Int(datanodeContainerPort),
					Protocol: pulumi.String("TCP"),
				},
			},
		},
	}, resOpts(provider)...)
	if err != nil {
		return fmt.Errorf("headless service: %w", err)
	}

	sts, err := appsv1.NewStatefulSet(ctx, datanodeStsName, &appsv1.StatefulSetArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(datanodeStsName),
			Namespace: pulumi.String(ns),
			Labels:    podLabels,
		},
		Spec: &appsv1.StatefulSetSpecArgs{
			Replicas:            pulumi.Int(cfg.NodeCount),
			PodManagementPolicy: pulumi.String("Parallel"),
			ServiceName:         pulumi.String(datanodeHeadlessSvcName),
			Selector: &metav1.LabelSelectorArgs{
				MatchLabels: podLabels,
			},
			Template: &corev1.PodTemplateSpecArgs{
				Metadata: &metav1.ObjectMetaArgs{
					Labels: podLabels,
				},
				Spec: &corev1.PodSpecArgs{
					ServiceAccountName:           pulumi.String(saDatanode),
					AutomountServiceAccountToken: pulumi.BoolPtr(false),
					Containers: corev1.ContainerArray{
						corev1.ContainerArgs{
							Name:  pulumi.String("datanode"),
							Image: pulumi.String(cfg.DataNodeImage),
							Ports: corev1.ContainerPortArray{
								corev1.ContainerPortArgs{
									ContainerPort: pulumi.Int(datanodeContainerPort),
									Protocol:      pulumi.String("TCP"),
								},
							},
							Resources: &corev1.ResourceRequirementsArgs{
								Requests: pulumi.StringMap{
									"cpu":    pulumi.String("4"),
									"memory": pulumi.String("8Gi"),
								},
								Limits: pulumi.StringMap{
									"cpu":    pulumi.String("4"),
									"memory": pulumi.String("8Gi"),
								},
							},
							LivenessProbe: &corev1.ProbeArgs{
								HttpGet: &corev1.HTTPGetActionArgs{
									Path: pulumi.String("/healthz"),
									Port: pulumi.Int(datanodeContainerPort),
								},
								InitialDelaySeconds: pulumi.Int(15),
							},
						},
					},
				},
			},
		},
	}, resOpts(provider, pulumi.Parent(svc))...)
	if err != nil {
		return fmt.Errorf("statefulset: %w", err)
	}

	if _, err := policyv1.NewPodDisruptionBudget(ctx, datanodePDBName, &policyv1.PodDisruptionBudgetArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(datanodePDBName),
			Namespace: pulumi.String(ns),
		},
		Spec: &policyv1.PodDisruptionBudgetSpecArgs{
			MinAvailable: pulumi.Int(datanodePDBMinAvail),
			Selector: &metav1.LabelSelectorArgs{
				MatchLabels: podLabels,
			},
		},
	}, resOpts(provider, pulumi.Parent(sts))...); err != nil {
		return fmt.Errorf("pdb: %w", err)
	}

	return nil
}
