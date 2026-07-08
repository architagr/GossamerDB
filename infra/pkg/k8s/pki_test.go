package k8s

import (
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

// TestNewPKI_registersClusterIssuer verifies the ClusterIssuer is created.
func TestNewPKI_registersClusterIssuer(t *testing.T) {
	mon := runPKI(t, validPKICfg())
	if !contains(mon.names, pkiClusterIssuerName) {
		t.Errorf("ClusterIssuer %q not registered; got: %v", pkiClusterIssuerName, mon.names)
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

			// secretName assertion
			gotSecret := sv["secretName"]
			if !gotSecret.IsString() || gotSecret.StringValue() != tc.secretName {
				t.Errorf("secretName = %v, want %q", gotSecret, tc.secretName)
			}

			// SAN assertion — dnsNames[0]
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
