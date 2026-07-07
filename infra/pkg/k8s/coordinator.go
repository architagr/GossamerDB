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
	coordinatorStsName         = "gossamerdb-coordinator"
	coordinatorHeadlessSvcName = "gossamerdb-coordinator-headless"
	coordinatorPDBName         = "gossamerdb-coordinator"
	coordinatorContainerPort   = 8080
	coordinatorPVCSize         = "10Gi"
	coordinatorPVCMountPath    = "/var/lib/gossamerdb/raft"
	coordinatorFIOAnnotation   = "gossamerdb.io/fio-floor"
	coordinatorFIOFloor        = "3000iops"
)

// coordinatorQuorum returns the minimum available pods required for Raft
// consensus: floor(n/2)+1. For 3 replicas → 2, for 5 → 3, for 7 → 4.
func coordinatorQuorum(replicas int) int {
	return replicas/2 + 1
}

// NewCoordinator creates the coordinator StatefulSet with a local-disk PVC for
// the Raft commit log, the headless Service for peer discovery, and a
// PodDisruptionBudget sized to the quorum of cfg.CoordinatorReplicas.
//
// cfg.CoordinatorImage must be non-empty. cfg.CoordinatorReplicas must be odd
// and >= 3 (validated by config.Validate before this is called).
// cfg.CoordinatorStorageClass controls the PVC storage class.
// provider may be nil under Pulumi's mock test harness.
func NewCoordinator(ctx *pulumi.Context, cfg *config.Config, provider *kubernetes.Provider) error {
	if cfg.CoordinatorImage == "" {
		return fmt.Errorf("config.CoordinatorImage must not be empty")
	}

	ns := namespaceName(cfg)

	podLabels := pulumi.StringMap{
		"app":     pulumi.String("gossamerdb"),
		"role":    pulumi.String("coordinator"),
		"cluster": pulumi.String(cfg.ClusterName),
	}

	svc, err := corev1.NewService(ctx, coordinatorHeadlessSvcName, &corev1.ServiceArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(coordinatorHeadlessSvcName),
			Namespace: pulumi.String(ns),
		},
		Spec: &corev1.ServiceSpecArgs{
			ClusterIP: pulumi.String("None"),
			Selector:  podLabels,
			Ports: corev1.ServicePortArray{
				corev1.ServicePortArgs{
					Name:     pulumi.String("raft"),
					Port:     pulumi.Int(coordinatorContainerPort),
					Protocol: pulumi.String("TCP"),
				},
			},
		},
	}, resOpts(provider)...)
	if err != nil {
		return fmt.Errorf("headless service: %w", err)
	}

	sts, err := appsv1.NewStatefulSet(ctx, coordinatorStsName, &appsv1.StatefulSetArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(coordinatorStsName),
			Namespace: pulumi.String(ns),
			Labels:    podLabels,
		},
		Spec: &appsv1.StatefulSetSpecArgs{
			Replicas:    pulumi.Int(cfg.CoordinatorReplicas),
			ServiceName: pulumi.String(coordinatorHeadlessSvcName),
			// Ordered startup is required for Raft leader election (HLD §5.1 KAD-1).
			// Do NOT set podManagementPolicy: Parallel.
			UpdateStrategy: &appsv1.StatefulSetUpdateStrategyArgs{
				Type: pulumi.String("RollingUpdate"),
				RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategyArgs{
					MaxUnavailable: pulumi.Int(1),
				},
			},
			Selector: &metav1.LabelSelectorArgs{
				MatchLabels: podLabels,
			},
			Template: &corev1.PodTemplateSpecArgs{
				Metadata: &metav1.ObjectMetaArgs{
					Labels: podLabels,
				},
				Spec: &corev1.PodSpecArgs{
					ServiceAccountName: pulumi.String(saCoordinator),
					Containers: corev1.ContainerArray{
						corev1.ContainerArgs{
							Name:  pulumi.String("coordinator"),
							Image: pulumi.String(cfg.CoordinatorImage),
							Ports: corev1.ContainerPortArray{
								corev1.ContainerPortArgs{
									ContainerPort: pulumi.Int(coordinatorContainerPort),
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
									Port: pulumi.Int(coordinatorContainerPort),
								},
								InitialDelaySeconds: pulumi.Int(15),
							},
							VolumeMounts: corev1.VolumeMountArray{
								corev1.VolumeMountArgs{
									Name:      pulumi.String("raft-log"),
									MountPath: pulumi.String(coordinatorPVCMountPath),
								},
							},
						},
					},
				},
			},
			VolumeClaimTemplates: corev1.PersistentVolumeClaimTypeArray{
				corev1.PersistentVolumeClaimTypeArgs{
					Metadata: &metav1.ObjectMetaArgs{
						Name: pulumi.String("raft-log"),
						Annotations: pulumi.StringMap{
							coordinatorFIOAnnotation: pulumi.String(coordinatorFIOFloor),
						},
					},
					Spec: &corev1.PersistentVolumeClaimSpecArgs{
						AccessModes: pulumi.StringArray{pulumi.String("ReadWriteOnce")},
						StorageClassName: pulumi.StringPtr(cfg.CoordinatorStorageClass),
						Resources: &corev1.VolumeResourceRequirementsArgs{
							Requests: pulumi.StringMap{
								"storage": pulumi.String(coordinatorPVCSize),
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

	if _, err := policyv1.NewPodDisruptionBudget(ctx, coordinatorPDBName, &policyv1.PodDisruptionBudgetArgs{
		Metadata: &metav1.ObjectMetaArgs{
			Name:      pulumi.String(coordinatorPDBName),
			Namespace: pulumi.String(ns),
		},
		Spec: &policyv1.PodDisruptionBudgetSpecArgs{
			MinAvailable: pulumi.Int(coordinatorQuorum(cfg.CoordinatorReplicas)),
			Selector: &metav1.LabelSelectorArgs{
				MatchLabels: podLabels,
			},
		},
	}, resOpts(provider, pulumi.Parent(sts))...); err != nil {
		return fmt.Errorf("pdb: %w", err)
	}

	return nil
}
