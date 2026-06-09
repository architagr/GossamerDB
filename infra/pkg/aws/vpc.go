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
		VpcId: vpc.ID().ToStringOutput().ToStringPtrOutput(),
		Tags:  pulumi.StringMap{"Name": pulumi.String(clusterName + "-igw")},
	})
	if err != nil {
		return nil, fmt.Errorf("igw: %w", err)
	}

	pubRT, err := awsec2.NewRouteTable(ctx, clusterName+"-pub-rt", &awsec2.RouteTableArgs{
		VpcId: vpc.ID(),
		Routes: awsec2.RouteTableRouteArray{
			awsec2.RouteTableRouteArgs{
				CidrBlock: pulumi.StringPtr("0.0.0.0/0"),
				GatewayId: igw.ID().ToStringOutput().ToStringPtrOutput(),
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
