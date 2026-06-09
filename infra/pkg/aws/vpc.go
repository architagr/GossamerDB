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

// createVPC is implemented in Task 3.
// Stub keeps the package compilable during TDD.
func createVPC(ctx *pulumi.Context, clusterName string) (*vpcOutputs, error) {
	_ = awsec2.NewVpc // ensure import used; replaced in Task 3
	return &vpcOutputs{}, nil
}
