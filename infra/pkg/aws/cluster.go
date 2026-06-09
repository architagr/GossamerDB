package aws

import (
	"strings"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"gossamerdb/infra/pkg/config"
)

// ClusterOutputs holds the Pulumi stack outputs for the EKS cluster.
type ClusterOutputs struct {
	ClusterName     pulumi.StringOutput
	ClusterEndpoint pulumi.StringOutput
	Kubeconfig      pulumi.StringOutput
	NodeRoleARN     pulumi.StringOutput
}

// nodeIAMPolicies returns the managed IAM policy ARNs required by EKS worker nodes.
func nodeIAMPolicies() []string {
	return []string{
		"arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy",
		"arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy",
		"arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly",
	}
}

// eksVersion strips the "v" prefix. EKS accepts "1.29" not "v1.29".
func eksVersion(k8sVersion string) string {
	return strings.TrimPrefix(k8sVersion, "v")
}

// NewCluster is implemented in Task 4.
// Stub keeps the package compilable during TDD.
func NewCluster(ctx *pulumi.Context, cfg *config.Config) (*ClusterOutputs, error) {
	if cfg.Env != config.EnvAWS {
		return nil, nil
	}
	return &ClusterOutputs{}, nil
}
