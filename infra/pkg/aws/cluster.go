package aws

import (
	"fmt"
	"strings"

	awsec2 "github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	awseks "github.com/pulumi/pulumi-aws/sdk/v6/go/aws/eks"
	awsiam "github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
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

const (
	clusterAssumePolicy = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"eks.amazonaws.com"},"Action":"sts:AssumeRole"}]}`
	nodeAssumePolicy    = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"Action":"sts:AssumeRole"}]}`
)

// kubeconfigTemplate is an aws-cli exec-credential kubeconfig.
// Sprintf args: ca-data, server, cluster-name × 5, cluster-name for exec arg.
const kubeconfigTemplate = `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: %s
    server: %s
  name: %s
contexts:
- context:
    cluster: %s
    user: aws
  name: %s
current-context: %s
kind: Config
preferences: {}
users:
- name: aws
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1beta1
      command: aws
      args: [eks, get-token, --cluster-name, %s]
`

// NewCluster provisions an EKS cluster with VPC, IAM roles, and managed node group.
// Returns nil, nil when cfg.Env != EnvAWS.
func NewCluster(ctx *pulumi.Context, cfg *config.Config) (*ClusterOutputs, error) {
	if cfg.Env != config.EnvAWS {
		return nil, nil
	}

	vpc, err := createVPC(ctx, cfg.ClusterName, cfg.AWSRegion)
	if err != nil {
		return nil, fmt.Errorf("vpc: %w", err)
	}

	// EKS cluster IAM role
	clusterRole, err := awsiam.NewRole(ctx, cfg.ClusterName+"-cluster-role", &awsiam.RoleArgs{
		AssumeRolePolicy: pulumi.String(clusterAssumePolicy),
	})
	if err != nil {
		return nil, fmt.Errorf("cluster role: %w", err)
	}
	if _, err = awsiam.NewRolePolicyAttachment(ctx, cfg.ClusterName+"-cluster-policy", &awsiam.RolePolicyAttachmentArgs{
		Role:      clusterRole.Name,
		PolicyArn: pulumi.String("arn:aws:iam::aws:policy/AmazonEKSClusterPolicy"),
	}); err != nil {
		return nil, fmt.Errorf("cluster policy: %w", err)
	}

	// Node IAM role + required policies
	nodeRole, err := awsiam.NewRole(ctx, cfg.ClusterName+"-node-role", &awsiam.RoleArgs{
		AssumeRolePolicy: pulumi.String(nodeAssumePolicy),
	})
	if err != nil {
		return nil, fmt.Errorf("node role: %w", err)
	}
	for i, arn := range nodeIAMPolicies() {
		if _, err = awsiam.NewRolePolicyAttachment(ctx, fmt.Sprintf("%s-node-policy-%d", cfg.ClusterName, i), &awsiam.RolePolicyAttachmentArgs{
			Role:      nodeRole.Name,
			PolicyArn: pulumi.String(arn),
		}); err != nil {
			return nil, fmt.Errorf("node policy %d: %w", i, err)
		}
	}

	// EKS cluster (all 4 subnets in VPC config)
	allSubnets := make(pulumi.StringArray, 0, 4)
	for _, s := range append(vpc.privateSubnets, vpc.publicSubnets...) {
		allSubnets = append(allSubnets, s)
	}
	cluster, err := awseks.NewCluster(ctx, cfg.ClusterName, &awseks.ClusterArgs{
		Version: pulumi.String(eksVersion(cfg.K8sVersion)),
		RoleArn: clusterRole.Arn,
		VpcConfig: awseks.ClusterVpcConfigArgs{
			SubnetIds:             allSubnets,
			EndpointPrivateAccess: pulumi.Bool(true),
			EndpointPublicAccess:  pulumi.Bool(true),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("eks cluster: %w", err)
	}

	// Launch template for gp3 EBS root volume
	lt, err := awsec2.NewLaunchTemplate(ctx, cfg.ClusterName+"-lt", &awsec2.LaunchTemplateArgs{
		BlockDeviceMappings: awsec2.LaunchTemplateBlockDeviceMappingArray{
			awsec2.LaunchTemplateBlockDeviceMappingArgs{
				DeviceName: pulumi.String("/dev/xvda"),
				Ebs: awsec2.LaunchTemplateBlockDeviceMappingEbsArgs{
					VolumeSize: pulumi.Int(50),
					VolumeType: pulumi.String("gp3"),
					Encrypted:  pulumi.StringPtr("true"),
				},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("launch template: %w", err)
	}

	// Managed node group (private subnets only)
	privSubnets := make(pulumi.StringArray, 0, 2)
	for _, s := range vpc.privateSubnets {
		privSubnets = append(privSubnets, s)
	}
	if _, err = awseks.NewNodeGroup(ctx, cfg.ClusterName+"-ng", &awseks.NodeGroupArgs{
		ClusterName:   cluster.Name,
		NodeRoleArn:   nodeRole.Arn,
		SubnetIds:     privSubnets,
		InstanceTypes: pulumi.StringArray{pulumi.String("t3.xlarge")},
		ScalingConfig: awseks.NodeGroupScalingConfigArgs{
			DesiredSize: pulumi.Int(cfg.NodeCount),
			MinSize:     pulumi.Int(1),
			MaxSize:     pulumi.Int(cfg.NodeCount + 2),
		},
		LaunchTemplate: awseks.NodeGroupLaunchTemplateArgs{
			Id: lt.ID().ToStringOutput().ToStringPtrOutput(),
			Version: lt.LatestVersion.ApplyT(func(v int) string {
				return fmt.Sprintf("%d", v)
			}).(pulumi.StringOutput),
		},
	}); err != nil {
		return nil, fmt.Errorf("node group: %w", err)
	}

	// Build kubeconfig from cluster outputs.
	// CertificateAuthority.Data() returns pulumi.StringPtrOutput; deref safely.
	caDataPtr := cluster.CertificateAuthority.Data()
	kubeconfig := pulumi.All(cluster.Name, cluster.Endpoint, caDataPtr).ApplyT(
		func(args []interface{}) (string, error) {
			name := args[0].(string)
			endpoint := args[1].(string)
			ca := ""
			if p, ok := args[2].(*string); ok && p != nil {
				ca = *p
			}
			return fmt.Sprintf(kubeconfigTemplate, ca, endpoint, name, name, name, name, name), nil
		}).(pulumi.StringOutput)

	out := &ClusterOutputs{
		ClusterName:     pulumi.String(cfg.ClusterName).ToStringOutput(),
		ClusterEndpoint: cluster.Endpoint,
		Kubeconfig:      kubeconfig,
		NodeRoleARN:     nodeRole.Arn,
	}
	ctx.Export("cluster_name", out.ClusterName)
	ctx.Export("cluster_endpoint", out.ClusterEndpoint)
	ctx.Export("kubeconfig", pulumi.ToSecret(out.Kubeconfig))
	ctx.Export("node_role_arn", out.NodeRoleARN)
	return out, nil
}
