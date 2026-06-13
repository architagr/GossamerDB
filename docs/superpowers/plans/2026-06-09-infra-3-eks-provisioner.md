# INFRA-3: AWS EKS Cluster Provisioner — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `infra/pkg/aws/` with VPC + EKS cluster + managed node group provisioning so `make infra-up ENV=aws` provisions GossamerDB on AWS EKS.

**Architecture:** New `infra/pkg/aws/` package (same `gossamerdb/infra` module). `vpc.go` owns VPC + 4 subnets + route table. `cluster.go` owns `ClusterOutputs`, IAM roles, EKS cluster, node group (gp3 EBS via launch template). Pure helpers (`subnetCIDR`, `nodeIAMPolicies`, `eksVersion`) are unit-testable without a Pulumi context. `NewCluster` returns `nil, nil` for non-aws envs. Uses native `pulumi-aws/sdk/v6` only (no pulumi-eks component). `cmd/main.go`'s `deployAWS` stub is replaced with a real call.

**Tech Stack:** `github.com/pulumi/pulumi-aws/sdk/v6/go/aws/{ec2,eks,iam}`, `github.com/pulumi/pulumi/sdk/v3/go/pulumi`, Go 1.25.8.

---

## File Map

| Path | Action | Responsibility |
|------|--------|----------------|
| `infra/go.mod` + `infra/go.sum` | Modify | Add `pulumi-aws/sdk/v6` dependency |
| `infra/pkg/aws/vpc.go` | Create | `vpcOutputs`, `subnetCIDR`, `createVPC` |
| `infra/pkg/aws/cluster.go` | Create | `ClusterOutputs`, `nodeIAMPolicies`, `eksVersion`, `NewCluster` |
| `infra/pkg/aws/cluster_test.go` | Create | Unit tests for all pure helpers + struct field check |
| `infra/cmd/main.go` | Modify | Replace `deployAWS` stub with `aws.NewCluster` call |
| `infra/Pulumi.aws.yaml` | Create | Stack config for `aws` env |

---

### Task 1: Add pulumi-aws SDK v6 dependency

**Files:**
- Modify: `infra/go.mod`, `infra/go.sum`

- [ ] **Step 1: Fetch the SDK**

```bash
cd infra && go get github.com/pulumi/pulumi-aws/sdk/v6
```

Expected: `go.mod` gains `require github.com/pulumi/pulumi-aws/sdk/v6 v6.x.x`; `go.sum` updated.

- [ ] **Step 2: Verify existing build still passes**

```bash
cd infra && go build ./...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add infra/go.mod infra/go.sum
git commit -m "chore(infra): add pulumi-aws/sdk/v6 dependency refs #15"
```

---

### Task 2: TDD — pure helpers

**Files:**
- Create: `infra/pkg/aws/cluster_test.go`
- Create: `infra/pkg/aws/vpc.go` (pure helper only, no Pulumi resources yet)
- Create: `infra/pkg/aws/cluster.go` (struct + helpers only, no Pulumi resources yet)

- [ ] **Step 1: Write failing tests**

Create `infra/pkg/aws/cluster_test.go`:

```go
package aws

import (
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func TestSubnetCIDR(t *testing.T) {
	tests := []struct {
		isPublic bool
		index    int
		want     string
	}{
		{false, 0, "10.0.1.0/24"},
		{false, 1, "10.0.2.0/24"},
		{true, 0, "10.0.101.0/24"},
		{true, 1, "10.0.102.0/24"},
	}
	for _, tc := range tests {
		got := subnetCIDR(tc.isPublic, tc.index)
		if got != tc.want {
			t.Errorf("subnetCIDR(%v, %d) = %q, want %q", tc.isPublic, tc.index, got, tc.want)
		}
	}
}

func TestNodeIAMPolicies(t *testing.T) {
	want := []string{
		"arn:aws:iam::aws:policy/AmazonEKSWorkerNodePolicy",
		"arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy",
		"arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly",
	}
	got := nodeIAMPolicies()
	if len(got) != len(want) {
		t.Fatalf("nodeIAMPolicies() = %d items, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("policy[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestEKSVersion(t *testing.T) {
	tests := []struct{ in, want string }{
		{"1.29", "1.29"},
		{"v1.29", "1.29"},
		{"1.29.2", "1.29.2"},
		{"v1.29.2", "1.29.2"},
	}
	for _, tc := range tests {
		if got := eksVersion(tc.in); got != tc.want {
			t.Errorf("eksVersion(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestClusterOutputs_hasRequiredFields is a compile-time assertion: if any
// field is renamed or its type changes to something other than
// pulumi.StringOutput, this test fails to compile.
func TestClusterOutputs_hasRequiredFields(t *testing.T) {
	var o ClusterOutputs
	var _ pulumi.StringOutput = o.ClusterName
	var _ pulumi.StringOutput = o.ClusterEndpoint
	var _ pulumi.StringOutput = o.Kubeconfig
	var _ pulumi.StringOutput = o.NodeRoleARN
}
```

- [ ] **Step 2: Run tests — expect FAIL**

```bash
cd infra && go test ./pkg/aws/...
```

Expected: `cannot find package "gossamerdb/infra/pkg/aws"` — package does not exist yet.

- [ ] **Step 3: Create `infra/pkg/aws/vpc.go` with `subnetCIDR` (no resources yet)**

```go
package aws

import (
	"fmt"

	awsec2 "github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type vpcOutputs struct {
	vpcID          pulumi.StringOutput
	privateSubnets []pulumi.StringOutput
	publicSubnets  []pulumi.StringOutput
}

// subnetCIDR returns the CIDR block for a GossamerDB EKS subnet.
// Private subnets: 10.0.1.0/24, 10.0.2.0/24 (index 0, 1).
// Public subnets:  10.0.101.0/24, 10.0.102.0/24 (index 0, 1).
func subnetCIDR(isPublic bool, index int) string {
	third := index + 1
	if isPublic {
		third = 100 + index + 1
	}
	return fmt.Sprintf("10.0.%d.0/24", third)
}

// createVPC is implemented in Task 3 below.
// Stub keeps the package compilable during Task 2 TDD.
func createVPC(ctx *pulumi.Context, clusterName string) (*vpcOutputs, error) {
	_ = awsec2.NewVpc // ensure import used; replaced in Task 3
	return &vpcOutputs{}, nil
}
```

- [ ] **Step 4: Create `infra/pkg/aws/cluster.go` with struct + helpers (no resources yet)**

```go
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

// NewCluster is implemented in Task 4 below.
// Stub keeps the package compilable during Task 2 TDD.
func NewCluster(ctx *pulumi.Context, cfg *config.Config) (*ClusterOutputs, error) {
	if cfg.Env != config.EnvAWS {
		return nil, nil
	}
	return &ClusterOutputs{}, nil
}
```

- [ ] **Step 5: Run tests — expect PASS**

```bash
cd infra && go test ./pkg/aws/...
```

Expected: `ok  gossamerdb/infra/pkg/aws`

- [ ] **Step 6: Commit**

```bash
git add infra/pkg/aws/
git commit -m "feat(infra): add pure helpers + ClusterOutputs struct (TDD) refs #15"
```

---

### Task 3: VPC resource implementation

**Files:**
- Modify: `infra/pkg/aws/vpc.go` (replace stub `createVPC`)

- [ ] **Step 1: Replace `createVPC` stub with full implementation**

Replace the entire content of `infra/pkg/aws/vpc.go` with:

```go
package aws

import (
	"fmt"

	awsec2 "github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type vpcOutputs struct {
	vpcID          pulumi.StringOutput
	privateSubnets []pulumi.StringOutput
	publicSubnets  []pulumi.StringOutput
}

// subnetCIDR returns the CIDR block for a GossamerDB EKS subnet.
// Private subnets: 10.0.1.0/24, 10.0.2.0/24 (index 0, 1).
// Public subnets:  10.0.101.0/24, 10.0.102.0/24 (index 0, 1).
func subnetCIDR(isPublic bool, index int) string {
	third := index + 1
	if isPublic {
		third = 100 + index + 1
	}
	return fmt.Sprintf("10.0.%d.0/24", third)
}

func createVPC(ctx *pulumi.Context, clusterName string) (*vpcOutputs, error) {
	vpc, err := awsec2.NewVpc(ctx, clusterName+"-vpc", &awsec2.VpcArgs{
		CidrBlock:          pulumi.String("10.0.0.0/16"),
		EnableDnsHostnames: pulumi.Bool(true),
		EnableDnsSupport:   pulumi.Bool(true),
		Tags:               pulumi.StringMap{"Name": pulumi.String(clusterName + "-vpc")},
	})
	if err != nil {
		return nil, fmt.Errorf("vpc: %w", err)
	}

	igw, err := awsec2.NewInternetGateway(ctx, clusterName+"-igw", &awsec2.InternetGatewayArgs{
		VpcId: vpc.ID(),
		Tags:  pulumi.StringMap{"Name": pulumi.String(clusterName + "-igw")},
	})
	if err != nil {
		return nil, fmt.Errorf("igw: %w", err)
	}

	pubRT, err := awsec2.NewRouteTable(ctx, clusterName+"-pub-rt", &awsec2.RouteTableArgs{
		VpcId: vpc.ID(),
		Routes: awsec2.RouteTableRouteArray{
			&awsec2.RouteTableRouteArgs{
				CidrBlock: pulumi.String("0.0.0.0/0"),
				GatewayId: igw.ID(),
			},
		},
		Tags: pulumi.StringMap{"Name": pulumi.String(clusterName + "-pub-rt")},
	})
	if err != nil {
		return nil, fmt.Errorf("public route table: %w", err)
	}

	var privateSubnets, publicSubnets []pulumi.StringOutput

	for i := 0; i < 2; i++ {
		priv, err := awsec2.NewSubnet(ctx, fmt.Sprintf("%s-priv-%d", clusterName, i), &awsec2.SubnetArgs{
			VpcId:     vpc.ID(),
			CidrBlock: pulumi.String(subnetCIDR(false, i)),
			Tags: pulumi.StringMap{
				"Name":                                  pulumi.String(fmt.Sprintf("%s-priv-%d", clusterName, i)),
				"kubernetes.io/role/internal-elb":       pulumi.String("1"),
				"kubernetes.io/cluster/" + clusterName: pulumi.String("shared"),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("private subnet %d: %w", i, err)
		}
		privateSubnets = append(privateSubnets, priv.ID().ToStringOutput())

		pub, err := awsec2.NewSubnet(ctx, fmt.Sprintf("%s-pub-%d", clusterName, i), &awsec2.SubnetArgs{
			VpcId:               vpc.ID(),
			CidrBlock:           pulumi.String(subnetCIDR(true, i)),
			MapPublicIpOnLaunch: pulumi.Bool(true),
			Tags: pulumi.StringMap{
				"Name":                                  pulumi.String(fmt.Sprintf("%s-pub-%d", clusterName, i)),
				"kubernetes.io/role/elb":                pulumi.String("1"),
				"kubernetes.io/cluster/" + clusterName: pulumi.String("shared"),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("public subnet %d: %w", i, err)
		}
		publicSubnets = append(publicSubnets, pub.ID().ToStringOutput())

		if _, err = awsec2.NewRouteTableAssociation(ctx, fmt.Sprintf("%s-pub-rta-%d", clusterName, i), &awsec2.RouteTableAssociationArgs{
			SubnetId:     pub.ID(),
			RouteTableId: pubRT.ID(),
		}); err != nil {
			return nil, fmt.Errorf("route table association %d: %w", i, err)
		}
	}

	return &vpcOutputs{
		vpcID:          vpc.ID().ToStringOutput(),
		privateSubnets: privateSubnets,
		publicSubnets:  publicSubnets,
	}, nil
}
```

- [ ] **Step 2: Build + test**

```bash
cd infra && go build ./... && go test ./pkg/aws/...
```

Expected: build clean, `ok  gossamerdb/infra/pkg/aws` (tests still pass — subnetCIDR unchanged).

- [ ] **Step 3: Commit**

```bash
git add infra/pkg/aws/vpc.go
git commit -m "feat(infra): implement createVPC with subnets + route table refs #15"
```

---

### Task 4: EKS cluster provisioner

**Files:**
- Modify: `infra/pkg/aws/cluster.go` (replace stub `NewCluster` with full implementation)

- [ ] **Step 1: Replace `cluster.go` with full implementation**

Replace the entire content of `infra/pkg/aws/cluster.go` with:

```go
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

// kubeconfigTemplate is an aws-cli exec-credential kubeconfig for the cluster.
// Fields (in order): ca-data, server, cluster-name × 5, cluster-name for exec arg.
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

	vpc, err := createVPC(ctx, cfg.ClusterName)
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
			SubnetIds: allSubnets,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("eks cluster: %w", err)
	}

	// Launch template for gp3 EBS root volume on worker nodes
	lt, err := awsec2.NewLaunchTemplate(ctx, cfg.ClusterName+"-lt", &awsec2.LaunchTemplateArgs{
		BlockDeviceMappings: awsec2.LaunchTemplateBlockDeviceMappingArray{
			&awsec2.LaunchTemplateBlockDeviceMappingArgs{
				DeviceName: pulumi.String("/dev/xvda"),
				Ebs: &awsec2.LaunchTemplateBlockDeviceMappingEbsArgs{
					VolumeSize: pulumi.Int(50),
					VolumeType: pulumi.String("gp3"),
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
		ScalingConfig: &awseks.NodeGroupScalingConfigArgs{
			DesiredSize: pulumi.Int(cfg.NodeCount),
			MinSize:     pulumi.Int(1),
			MaxSize:     pulumi.Int(cfg.NodeCount + 2),
		},
		LaunchTemplate: &awseks.NodeGroupLaunchTemplateArgs{
			Id:      lt.ID(),
			Version: pulumi.Sprintf("%v", lt.LatestVersion),
		},
	}); err != nil {
		return nil, fmt.Errorf("node group: %w", err)
	}

	// Build kubeconfig from cluster outputs (no real AWS call at plan time)
	caData := cluster.CertificateAuthority.Data()
	kubeconfig := pulumi.All(cluster.Name, cluster.Endpoint, caData).ApplyT(
		func(args []interface{}) (string, error) {
			name, endpoint, ca := args[0].(string), args[1].(string), args[2].(string)
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
	ctx.Export("kubeconfig", out.Kubeconfig)
	ctx.Export("node_role_arn", out.NodeRoleARN)
	return out, nil
}
```

- [ ] **Step 2: Build + test**

```bash
cd infra && go build ./... && go test ./pkg/aws/...
```

Expected: build clean, tests pass.

- [ ] **Step 3: Run go vet**

```bash
cd infra && go vet ./...
```

Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add infra/pkg/aws/cluster.go
git commit -m "feat(infra): implement NewCluster — EKS + VPC + IAM + node group refs #15"
```

---

### Task 5: Wire `deployAWS` in cmd/main.go

**Files:**
- Modify: `infra/cmd/main.go`

- [ ] **Step 1: Replace `deployAWS` stub**

In `infra/cmd/main.go`, replace:

```go
import (
	"fmt"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"gossamerdb/infra/pkg/config"
	"gossamerdb/infra/pkg/local"
)
```

with:

```go
import (
	"fmt"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"gossamerdb/infra/pkg/aws"
	"gossamerdb/infra/pkg/config"
	"gossamerdb/infra/pkg/local"
)
```

Then replace the `deployAWS` function:

```go
// deployAWS provisions an EKS cluster on AWS via INFRA-3.
func deployAWS(ctx *pulumi.Context, cfg config.Config) error {
	_, err := aws.NewCluster(ctx, &cfg)
	return err
}
```

- [ ] **Step 2: Build**

```bash
cd infra && go build ./...
```

Expected: no errors.

- [ ] **Step 3: Run vet + tests**

```bash
cd infra && go vet ./... && go test ./...
```

Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add infra/cmd/main.go
git commit -m "feat(infra): wire deployAWS to aws.NewCluster fixes #15"
```

---

### Task 6: Pulumi.aws.yaml stack config

**Files:**
- Create: `infra/Pulumi.aws.yaml`

- [ ] **Step 1: Create stack config**

Create `infra/Pulumi.aws.yaml`:

```yaml
config:
  gossamerdb-infra:env: aws
  gossamerdb-infra:clusterName: gossamerdb-prod
  gossamerdb-infra:k8sVersion: "1.29"
  gossamerdb-infra:nodeCount: 3
  gossamerdb-infra:awsRegion: us-east-1
  gossamerdb-infra:namespace: gossamerdb
```

- [ ] **Step 2: Commit**

```bash
git add infra/Pulumi.aws.yaml
git commit -m "chore(infra): add Pulumi.aws.yaml stack config refs #15"
```

---

### Task 7: Final verify + draft PR

- [ ] **Step 1: Full build + vet + test from infra/**

```bash
cd infra && go build ./... && go vet ./... && go test ./...
```

Expected:
```
ok  gossamerdb/infra/pkg/aws
ok  gossamerdb/infra/pkg/config
ok  gossamerdb/infra/pkg/local
```
No errors from build or vet.

- [ ] **Step 2: Run golangci-lint**

```bash
cd infra && golangci-lint run ./...
```

Expected: no issues (or only warnings about unused imports in stubs, but there are no stubs at this point).

- [ ] **Step 3: Open draft PR targeting `feat/12-iac-pulumi-go`**

```bash
git push -u origin feat/15-infra-3-eks-provisioner
```

Then open PR:

```bash
./.claude/scripts/github-api.sh create-draft-pr \
  "feat(infra): INFRA-3 AWS EKS cluster provisioner (Pulumi Go)" \
  "## Summary

- Adds \`infra/pkg/aws/\` package with VPC provisioner and EKS cluster provisioner
- VPC: 2 private + 2 public subnets (10.0.0.0/16), IGW + public route table, EKS subnet tags
- EKS: cluster IAM role + AmazonEKSClusterPolicy, node IAM role + 3 required policies, managed node group with t3.xlarge + gp3 EBS via launch template
- \`NewCluster\` returns \`nil, nil\` for non-aws envs (guard clause)
- \`cmd/main.go\` deployAWS stub replaced with live \`aws.NewCluster\` call
- Unit tests cover all pure helpers (\`subnetCIDR\`, \`nodeIAMPolicies\`, \`eksVersion\`) and \`ClusterOutputs\` struct fields

Fixes #15" \
  feat/15-infra-3-eks-provisioner \
  feat/12-iac-pulumi-go
```

Expected: PR URL printed.

---

## Acceptance Criteria Checklist (from issue #15)

- [ ] `infra/pkg/aws/cluster.go` — `NewCluster(ctx *pulumi.Context, cfg *config.Config) (*ClusterOutputs, error)`
- [ ] VPC: 2 private + 2 public subnets in `cfg.AWSRegion`; CIDR `10.0.0.0/16`
- [ ] EKS cluster: Kubernetes version matches `cfg.K8sVersion`
- [ ] Managed node group: `cfg.NodeCount` nodes, `t3.xlarge`, EBS GP3 volume (launch template)
- [ ] Node IAM role with `AmazonEKSWorkerNodePolicy`, `AmazonEKS_CNI_Policy`, `AmazonEC2ContainerRegistryReadOnly`
- [ ] Pulumi outputs: `cluster_name`, `cluster_endpoint`, `kubeconfig`, `node_role_arn`
- [ ] `infra/pkg/aws/cluster_test.go` — unit tests with no real AWS call; `ClusterOutputs` struct fields verified
- [ ] `NewCluster` returns `nil, nil` when `cfg.Env != "aws"`
- [ ] `go build ./...` + `go vet ./...` + `go test ./...` all green from `infra/`
