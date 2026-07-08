package k8s

import (
	"fmt"

	"gossamerdb/infra/pkg/config"

	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apiextensions"
	apiextv1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/apiextensions/v1"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const (
	pkiClusterIssuerName    = "gossamerdb-ca-issuer"
	pkiCoordinatorTLSSecret = "gossamerdb-coordinator-tls"
	pkiDatanodeTLSSecret    = "gossamerdb-datanode-tls"
	pkiAdminTLSSecret       = "gossamerdb-admin-tls"
	pkiClientTLSSecret      = "gossamerdb-client-tls"
	pkiCertDuration    = "8760h"
	pkiCertRenewBefore = "720h"
	pkiCertManagerCRD  = "certificates.cert-manager.io"
)

// certDef describes a single Certificate resource.
type certDef struct {
	secretName string
	san        string
	role       string
}

// NewPKI provisions cert-manager PKI resources: a ClusterIssuer backed by the
// operator-supplied CA Secret, and four Certificate resources matching the
// GossamerDB SAN identity scheme (HLD §5.7).
//
// Returns an error if CASecretName is empty or cert-manager CRDs are absent.
func NewPKI(ctx *pulumi.Context, cfg *config.Config, provider *kubernetes.Provider) error {
	if cfg.CASecretName == "" {
		return fmt.Errorf("config.CASecretName must not be empty: supply the name of the cert-manager CA Secret")
	}

	crdCheck, err := checkCertManagerCRDs(ctx, provider)
	if err != nil {
		return err
	}

	issuer, err := createClusterIssuer(ctx, cfg, provider, crdCheck)
	if err != nil {
		return err
	}

	return createCertificates(ctx, cfg, provider, issuer)
}

// checkCertManagerCRDs reads the certificates.cert-manager.io CRD from the cluster.
// Pulumi's ReadResource runs asynchronously; if the CRD is absent the deploy fails
// with "cert-manager-crd-check" as the failed resource. The returned resource is
// passed as DependsOn to the ClusterIssuer so downstream resources are blocked.
// Operator guidance: https://cert-manager.io/docs/installation/
func checkCertManagerCRDs(ctx *pulumi.Context, provider *kubernetes.Provider) (*apiextv1.CustomResourceDefinition, error) {
	crd, err := apiextv1.GetCustomResourceDefinition(ctx, "cert-manager-crd-check",
		pulumi.ID(pkiCertManagerCRD),
		nil,
		resOpts(provider)...,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"cert-manager CRDs not installed: deploy cert-manager first — https://cert-manager.io/docs/installation/",
		)
	}
	return crd, nil
}

func createClusterIssuer(
	ctx *pulumi.Context,
	cfg *config.Config,
	provider *kubernetes.Provider,
	crdCheck *apiextv1.CustomResourceDefinition,
) (*apiextensions.CustomResource, error) {
	return apiextensions.NewCustomResource(ctx, pkiClusterIssuerName, &apiextensions.CustomResourceArgs{
		ApiVersion: pulumi.String("cert-manager.io/v1"),
		Kind:       pulumi.String("ClusterIssuer"),
		Metadata: &metav1.ObjectMetaArgs{
			Name: pulumi.String(pkiClusterIssuerName),
		},
		OtherFields: kubernetes.UntypedArgs{
			"spec": pulumi.Map{
				"ca": pulumi.Map{
					"secretName": pulumi.String(cfg.CASecretName),
				},
			},
		},
	}, resOpts(provider, pulumi.DependsOn([]pulumi.Resource{crdCheck}))...)
}

func createCertificates(
	ctx *pulumi.Context,
	cfg *config.Config,
	provider *kubernetes.Provider,
	issuer *apiextensions.CustomResource,
) error {
	ns := namespaceName(cfg)
	defs := []certDef{
		{pkiCoordinatorTLSSecret, fmt.Sprintf("coordinator.%s", cfg.ClusterName), "coordinator"},
		{pkiDatanodeTLSSecret, fmt.Sprintf("data.%s.%s", cfg.ClusterName, cfg.Region), "datanode"},
		{pkiAdminTLSSecret, fmt.Sprintf("admin.%s", cfg.ClusterName), "admin"},
		{pkiClientTLSSecret, fmt.Sprintf("client.%s", cfg.ClusterName), "client"},
	}

	opts := resOpts(provider, pulumi.DependsOn([]pulumi.Resource{issuer}))

	for _, d := range defs {
		if _, err := apiextensions.NewCustomResource(ctx, d.secretName, &apiextensions.CustomResourceArgs{
			ApiVersion: pulumi.String("cert-manager.io/v1"),
			Kind:       pulumi.String("Certificate"),
			Metadata: &metav1.ObjectMetaArgs{
				Name:      pulumi.String(d.secretName),
				Namespace: pulumi.String(ns),
			},
			OtherFields: kubernetes.UntypedArgs{
				"spec": pulumi.Map{
					"secretName":  pulumi.String(d.secretName),
					"duration":    pulumi.String(pkiCertDuration),
					"renewBefore": pulumi.String(pkiCertRenewBefore),
					"dnsNames":    pulumi.StringArray{pulumi.String(d.san)},
					"privateKey": pulumi.Map{
						"algorithm": pulumi.String("ECDSA"),
						"size":      pulumi.Int(256),
					},
					"subject": pulumi.Map{
						"organizationalUnits": pulumi.StringArray{
							pulumi.String("role=" + d.role),
						},
					},
					"issuerRef": pulumi.Map{
						"name":  pulumi.String(pkiClusterIssuerName),
						"kind":  pulumi.String("ClusterIssuer"),
						"group": pulumi.String("cert-manager.io"),
					},
				},
			},
		}, opts...); err != nil {
			return fmt.Errorf("certificate %q: %w", d.secretName, err)
		}
	}
	return nil
}
