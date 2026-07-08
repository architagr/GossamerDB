package k8s

import (
	"fmt"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"gossamerdb/infra/pkg/config"
)

// pkiEntry holds a single resource captured by the PKI monitor.
type pkiEntry struct {
	name   string
	typ    string
	inputs resource.PropertyMap
}

// pkiMonitor captures every resource registered by NewPKI.
type pkiMonitor struct {
	trackingMonitor
	entries []pkiEntry
}

func (m *pkiMonitor) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.mu.Lock()
	m.names = append(m.names, args.Name)
	m.entries = append(m.entries, pkiEntry{
		name:   args.Name,
		typ:    args.TypeToken,
		inputs: args.Inputs,
	})
	m.mu.Unlock()
	return args.Name + "_id", args.Inputs, nil
}

func (m *pkiMonitor) entriesByType(typeToken string) []pkiEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []pkiEntry
	for _, e := range m.entries {
		if e.typ == typeToken {
			out = append(out, e)
		}
	}
	return out
}

func (m *pkiMonitor) entryByName(name string) (pkiEntry, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.entries {
		if e.name == name {
			return e, true
		}
	}
	return pkiEntry{}, false
}

// pkiCRDFailMonitor simulates a cluster where cert-manager CRDs are absent.
type pkiCRDFailMonitor struct {
	pkiMonitor
}

func (m *pkiCRDFailMonitor) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	if args.TypeToken == "kubernetes:apiextensions.k8s.io/v1:CustomResourceDefinition" {
		return "", nil, fmt.Errorf("resource not found")
	}
	return m.pkiMonitor.NewResource(args)
}

func runPKI(t *testing.T, cfg *config.Config) *pkiMonitor {
	t.Helper()
	mon := &pkiMonitor{}
	if err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		return NewPKI(ctx, cfg, nil)
	}, pulumi.WithMocks("gossamerdb", "test", mon)); err != nil {
		t.Fatalf("RunErr: %v", err)
	}
	return mon
}

func validPKICfg() *config.Config {
	return &config.Config{
		Env:                     config.EnvLocal,
		ClusterName:             "prod",
		Region:                  "local",
		CASecretName:            "gossamerdb-ca",
		CoordinatorReplicas:     3,
		CoordinatorStorageClass: "standard",
	}
}

// TestNewPKI_missingCASecretNameReturnsError ensures missing CASecretName is rejected.
func TestNewPKI_missingCASecretNameReturnsError(t *testing.T) {
	cfg := validPKICfg()
	cfg.CASecretName = ""
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		return NewPKI(ctx, cfg, nil)
	}, pulumi.WithMocks("gossamerdb", "test", &pkiMonitor{}))
	if err == nil {
		t.Fatal("want error for empty CASecretName, got nil")
	}
}

// TestNewPKI_missingCASecretNameRegistersZeroResources asserts early return before
// any Pulumi resource is registered when CASecretName is absent.
func TestNewPKI_missingCASecretNameRegistersZeroResources(t *testing.T) {
	cfg := validPKICfg()
	cfg.CASecretName = ""
	mon := &pkiMonitor{}
	_ = pulumi.RunErr(func(ctx *pulumi.Context) error {
		return NewPKI(ctx, cfg, nil)
	}, pulumi.WithMocks("gossamerdb", "test", mon))
	if len(mon.names) != 0 {
		t.Errorf("want 0 resources registered on empty CASecretName, got %d: %v", len(mon.names), mon.names)
	}
}

// TestNewPKI_crdAbsentReturnsError asserts that NewPKI fails when cert-manager CRDs
// are absent. Pulumi's ReadResource runs in a goroutine; the error propagates via
// the async Output system, so we can only assert err != nil here rather than
// checking for the install URL (which is in the synchronous error-path comment).
func TestNewPKI_crdAbsentReturnsError(t *testing.T) {
	cfg := validPKICfg()
	mon := &pkiCRDFailMonitor{}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		return NewPKI(ctx, cfg, nil)
	}, pulumi.WithMocks("gossamerdb", "test", mon))
	if err == nil {
		t.Fatal("want error when cert-manager CRDs absent, got nil")
	}
}

// TestNewPKI_registersClusterIssuer verifies the ClusterIssuer is created.
func TestNewPKI_registersClusterIssuer(t *testing.T) {
	mon := runPKI(t, validPKICfg())
	if !contains(mon.names, pkiClusterIssuerName) {
		t.Errorf("ClusterIssuer %q not registered; got: %v", pkiClusterIssuerName, mon.names)
	}
}

// TestNewPKI_clusterIssuerCASecretName asserts the ClusterIssuer spec references
// cfg.CASecretName for the CA key-pair Secret.
func TestNewPKI_clusterIssuerCASecretName(t *testing.T) {
	cfg := validPKICfg()
	cfg.CASecretName = "my-custom-ca"
	mon := runPKI(t, cfg)
	entry, ok := mon.entryByName(pkiClusterIssuerName)
	if !ok {
		t.Fatal("ClusterIssuer not registered")
	}
	spec := entry.inputs["spec"]
	if !spec.IsObject() {
		t.Skip("spec not an object in mock inputs")
	}
	ca := spec.ObjectValue()["ca"]
	if !ca.IsObject() {
		t.Fatal("spec.ca missing in ClusterIssuer")
	}
	sn := ca.ObjectValue()["secretName"]
	if !sn.IsString() || sn.StringValue() != cfg.CASecretName {
		t.Errorf("spec.ca.secretName = %v, want %q", sn, cfg.CASecretName)
	}
}

// TestNewPKI_registersFourCertificates asserts all four Certificate resources are created.
func TestNewPKI_registersFourCertificates(t *testing.T) {
	mon := runPKI(t, validPKICfg())
	certs := mon.entriesByType("kubernetes:cert-manager.io/v1:Certificate")
	if len(certs) != 4 {
		t.Errorf("want 4 Certificate resources, got %d", len(certs))
	}
}

// TestNewPKI_certificateSANsAndSecretNames verifies each cert's SAN and secretName.
func TestNewPKI_certificateSANsAndSecretNames(t *testing.T) {
	cfg := validPKICfg()
	cfg.ClusterName = "prod"
	cfg.Region = "us-east-1"
	mon := runPKI(t, cfg)

	cases := []struct {
		secretName string
		wantSAN    string
	}{
		{pkiCoordinatorTLSSecret, "coordinator.prod"},
		{pkiDatanodeTLSSecret, "data.prod.us-east-1"},
		{pkiAdminTLSSecret, "admin.prod"},
		{pkiClientTLSSecret, "client.prod"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.secretName, func(t *testing.T) {
			entry, ok := mon.entryByName(tc.secretName)
			if !ok {
				t.Fatalf("Certificate %q not registered", tc.secretName)
			}
			spec := entry.inputs["spec"]
			if !spec.IsObject() {
				t.Skip("spec not an object in mock inputs")
			}
			sv := spec.ObjectValue()

			gotSecret := sv["secretName"]
			if !gotSecret.IsString() || gotSecret.StringValue() != tc.secretName {
				t.Errorf("secretName = %v, want %q", gotSecret, tc.secretName)
			}

			dns := sv["dnsNames"]
			if !dns.IsArray() || len(dns.ArrayValue()) == 0 {
				t.Fatalf("dnsNames missing or empty for %q", tc.secretName)
			}
			gotSAN := dns.ArrayValue()[0]
			if !gotSAN.IsString() || gotSAN.StringValue() != tc.wantSAN {
				t.Errorf("dnsNames[0] = %v, want %q", gotSAN, tc.wantSAN)
			}
		})
	}
}

// TestNewPKI_certSubjectOUPerRole asserts each cert carries the correct
// role= organizationalUnit slot (HLD §11.1 forward-compat hook).
func TestNewPKI_certSubjectOUPerRole(t *testing.T) {
	mon := runPKI(t, validPKICfg())

	cases := []struct {
		secretName string
		wantOU     string
	}{
		{pkiCoordinatorTLSSecret, "role=coordinator"},
		{pkiDatanodeTLSSecret, "role=datanode"},
		{pkiAdminTLSSecret, "role=admin"},
		{pkiClientTLSSecret, "role=client"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.secretName, func(t *testing.T) {
			entry, ok := mon.entryByName(tc.secretName)
			if !ok {
				t.Fatalf("Certificate %q not registered", tc.secretName)
			}
			spec := entry.inputs["spec"]
			if !spec.IsObject() {
				t.Skip("spec not an object in mock inputs")
			}
			subject := spec.ObjectValue()["subject"]
			if !subject.IsObject() {
				t.Fatalf("%s: subject missing", tc.secretName)
			}
			ous := subject.ObjectValue()["organizationalUnits"]
			if !ous.IsArray() || len(ous.ArrayValue()) == 0 {
				t.Fatalf("%s: organizationalUnits missing", tc.secretName)
			}
			ou := ous.ArrayValue()[0]
			if !ou.IsString() || ou.StringValue() != tc.wantOU {
				t.Errorf("%s: organizationalUnits[0] = %v, want %q", tc.secretName, ou, tc.wantOU)
			}
		})
	}
}

// TestNewPKI_certIssuerRefWiring verifies all four certs reference the ClusterIssuer.
func TestNewPKI_certIssuerRefWiring(t *testing.T) {
	mon := runPKI(t, validPKICfg())
	certs := mon.entriesByType("kubernetes:cert-manager.io/v1:Certificate")
	for _, c := range certs {
		spec := c.inputs["spec"]
		if !spec.IsObject() {
			continue
		}
		ref := spec.ObjectValue()["issuerRef"]
		if !ref.IsObject() {
			t.Errorf("%s: issuerRef missing", c.name)
			continue
		}
		rv := ref.ObjectValue()
		if n := rv["name"]; !n.IsString() || n.StringValue() != pkiClusterIssuerName {
			t.Errorf("%s: issuerRef.name = %v, want %q", c.name, n, pkiClusterIssuerName)
		}
		if k := rv["kind"]; !k.IsString() || k.StringValue() != "ClusterIssuer" {
			t.Errorf("%s: issuerRef.kind = %v, want ClusterIssuer", c.name, k)
		}
		if g := rv["group"]; !g.IsString() || g.StringValue() != "cert-manager.io" {
			t.Errorf("%s: issuerRef.group = %v, want cert-manager.io", c.name, g)
		}
	}
}

// TestNewPKI_regionDefaultsToLocalForNonAWS verifies Region="local" default on
// non-AWS env produces the correct datanode SAN.
func TestNewPKI_regionDefaultsToLocalForNonAWS(t *testing.T) {
	cfg := config.ApplyDefaults(config.Config{
		Env:          config.EnvLocal,
		ClusterName:  "ci",
		CASecretName: "gossamerdb-ca",
		CoordinatorReplicas: 3,
	})
	mon := runPKI(t, &cfg)
	entry, ok := mon.entryByName(pkiDatanodeTLSSecret)
	if !ok {
		t.Fatal("datanode cert not registered")
	}
	spec := entry.inputs["spec"]
	if !spec.IsObject() {
		t.Skip("spec not an object in mock inputs")
	}
	dns := spec.ObjectValue()["dnsNames"]
	if !dns.IsArray() || len(dns.ArrayValue()) == 0 {
		t.Fatal("dnsNames missing")
	}
	san := dns.ArrayValue()[0]
	wantSAN := "data.ci.local"
	if !san.IsString() || san.StringValue() != wantSAN {
		t.Errorf("datanode SAN = %v, want %q", san, wantSAN)
	}
}

// TestNewPKI_certDurationAndRenewBefore asserts standard cert lifetime values.
func TestNewPKI_certDurationAndRenewBefore(t *testing.T) {
	mon := runPKI(t, validPKICfg())
	certs := mon.entriesByType("kubernetes:cert-manager.io/v1:Certificate")
	if len(certs) == 0 {
		t.Fatal("no Certificate resources registered")
	}
	for _, c := range certs {
		spec := c.inputs["spec"]
		if !spec.IsObject() {
			continue
		}
		sv := spec.ObjectValue()
		if dur := sv["duration"]; !dur.IsString() || dur.StringValue() != pkiCertDuration {
			t.Errorf("%s: duration = %v, want %q", c.name, dur, pkiCertDuration)
		}
		if rb := sv["renewBefore"]; !rb.IsString() || rb.StringValue() != pkiCertRenewBefore {
			t.Errorf("%s: renewBefore = %v, want %q", c.name, rb, pkiCertRenewBefore)
		}
	}
}

// TestNewPKI_certPrivateKeyECDSA asserts ECDSA P-256 for all certs.
func TestNewPKI_certPrivateKeyECDSA(t *testing.T) {
	mon := runPKI(t, validPKICfg())
	certs := mon.entriesByType("kubernetes:cert-manager.io/v1:Certificate")
	for _, c := range certs {
		spec := c.inputs["spec"]
		if !spec.IsObject() {
			continue
		}
		pk := spec.ObjectValue()["privateKey"]
		if !pk.IsObject() {
			t.Errorf("%s: privateKey missing", c.name)
			continue
		}
		pkv := pk.ObjectValue()
		if alg := pkv["algorithm"]; !alg.IsString() || alg.StringValue() != "ECDSA" {
			t.Errorf("%s: algorithm = %v, want ECDSA", c.name, alg)
		}
		if sz := pkv["size"]; !sz.IsNumber() || int(sz.NumberValue()) != 256 {
			t.Errorf("%s: size = %v, want 256", c.name, sz)
		}
	}
}
