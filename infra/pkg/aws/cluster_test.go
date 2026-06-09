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
	var _ pulumi.StringOutput = o.ClusterName     //nolint:staticcheck // explicit type is the compile-time assertion
	var _ pulumi.StringOutput = o.ClusterEndpoint //nolint:staticcheck // explicit type is the compile-time assertion
	var _ pulumi.StringOutput = o.Kubeconfig      //nolint:staticcheck // explicit type is the compile-time assertion
	var _ pulumi.StringOutput = o.NodeRoleARN     //nolint:staticcheck // explicit type is the compile-time assertion
}
