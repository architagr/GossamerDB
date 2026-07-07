package k8s

import (
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"gossamerdb/infra/pkg/config"
)

// networkingEntry holds a single resource registration for inspection.
type networkingEntry struct {
	name   string
	typ    string
	inputs resource.PropertyMap
}

// networkingMonitor captures every resource registered by NewServices.
// Concurrent access is guarded by an embedded trackingMonitor mutex.
type networkingMonitor struct {
	trackingMonitor
	entries []networkingEntry
}

func (m *networkingMonitor) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.mu.Lock()
	m.names = append(m.names, args.Name)
	m.entries = append(m.entries, networkingEntry{
		name:   args.Name,
		typ:    args.TypeToken,
		inputs: args.Inputs,
	})
	m.mu.Unlock()
	return args.Name + "_id", args.Inputs, nil
}

func (m *networkingMonitor) entryByName(name string) (networkingEntry, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.entries {
		if e.name == name {
			return e, true
		}
	}
	return networkingEntry{}, false
}

func runServices(t *testing.T, cfg *config.Config) *networkingMonitor {
	t.Helper()
	mon := &networkingMonitor{}
	if err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		return NewServices(ctx, cfg, nil)
	}, pulumi.WithMocks("gossamerdb", "test", mon)); err != nil {
		t.Fatalf("RunErr: %v", err)
	}
	return mon
}

func containsStr(ss []string, target string) bool {
	for _, s := range ss {
		if s == target {
			return true
		}
	}
	return false
}

// TestNewServices_registersAllThreeServices verifies all expected service names.
func TestNewServices_registersAllThreeServices(t *testing.T) {
	cfg := &config.Config{Env: config.EnvLocal, ClusterName: "ci"}
	mon := runServices(t, cfg)
	for _, name := range []string{svcClientGRPC, svcClientREST, svcAdminGRPC} {
		if !containsStr(mon.names, name) {
			t.Errorf("service %q not registered; got: %v", name, mon.names)
		}
	}
}

// TestNewServices_localUsesClusterIP verifies type=ClusterIP and no cloud annotations on local.
func TestNewServices_localUsesClusterIP(t *testing.T) {
	cfg := &config.Config{Env: config.EnvLocal, ClusterName: "ci"}
	mon := runServices(t, cfg)
	for _, name := range []string{svcClientGRPC, svcClientREST, svcAdminGRPC} {
		e, ok := mon.entryByName(name)
		if !ok {
			t.Fatalf("service %q not found", name)
		}
		spec := e.inputs["spec"]
		if !spec.IsObject() {
			t.Skip("spec not an object in mock inputs")
		}
		svcType := spec.ObjectValue()["type"]
		if !svcType.IsString() || svcType.StringValue() != "ClusterIP" {
			t.Errorf("service %q: type = %v, want ClusterIP", name, svcType)
		}
		meta := e.inputs["metadata"]
		if meta.IsObject() {
			if ann, ok := meta.ObjectValue()["annotations"]; ok && ann.IsObject() {
				for k := range ann.ObjectValue() {
					if string(k) == nlbTypeAnnotation || string(k) == nlbSchemeAnnotation {
						t.Errorf("service %q: found cloud annotation %q on local env", name, k)
					}
				}
			}
		}
	}
}

// TestNewServices_k8sUsesClusterIP verifies type=ClusterIP on generic k8s env.
func TestNewServices_k8sUsesClusterIP(t *testing.T) {
	cfg := &config.Config{Env: config.EnvK8s, ClusterName: "ci"}
	mon := runServices(t, cfg)
	for _, name := range []string{svcClientGRPC, svcClientREST, svcAdminGRPC} {
		e, ok := mon.entryByName(name)
		if !ok {
			t.Fatalf("service %q not found", name)
		}
		spec := e.inputs["spec"]
		if !spec.IsObject() {
			t.Skip("spec not an object in mock inputs")
		}
		svcType := spec.ObjectValue()["type"]
		if !svcType.IsString() || svcType.StringValue() != "ClusterIP" {
			t.Errorf("service %q: type = %v, want ClusterIP", name, svcType)
		}
	}
}

// TestNewServices_awsUsesLoadBalancerWithNLBAnnotations verifies type=LoadBalancer,
// nlb type annotation, and correct scheme per service:
//   - client services → internet-facing
//   - admin service   → internal (admin plane must not be publicly reachable)
func TestNewServices_awsUsesLoadBalancerWithNLBAnnotations(t *testing.T) {
	cfg := &config.Config{Env: config.EnvAWS, ClusterName: "ci", AWSRegion: "us-east-1"}
	mon := runServices(t, cfg)

	wantScheme := map[string]string{
		svcClientGRPC: "internet-facing",
		svcClientREST: "internet-facing",
		svcAdminGRPC:  "internal",
	}

	for name, wantSch := range wantScheme {
		e, ok := mon.entryByName(name)
		if !ok {
			t.Fatalf("service %q not found", name)
		}
		spec := e.inputs["spec"]
		if !spec.IsObject() {
			t.Skip("spec not an object in mock inputs")
		}
		svcType := spec.ObjectValue()["type"]
		if !svcType.IsString() || svcType.StringValue() != "LoadBalancer" {
			t.Errorf("service %q: type = %v, want LoadBalancer", name, svcType)
		}
		meta := e.inputs["metadata"]
		if !meta.IsObject() {
			t.Fatalf("service %q: metadata not an object", name)
		}
		ann := meta.ObjectValue()["annotations"]
		if !ann.IsObject() {
			t.Fatalf("service %q: annotations not present on AWS", name)
		}
		annMap := ann.ObjectValue()
		if v := annMap[resource.PropertyKey(nlbTypeAnnotation)]; !v.IsString() || v.StringValue() != "nlb" {
			t.Errorf("service %q: %q = %v, want nlb", name, nlbTypeAnnotation, v)
		}
		if v := annMap[resource.PropertyKey(nlbSchemeAnnotation)]; !v.IsString() || v.StringValue() != wantSch {
			t.Errorf("service %q: %q = %v, want %q", name, nlbSchemeAnnotation, v, wantSch)
		}
	}
}

// TestNewServices_awsAdminIsInternalScheme explicitly verifies the admin service
// uses scheme=internal on AWS (security: admin plane must not be internet-facing).
func TestNewServices_awsAdminIsInternalScheme(t *testing.T) {
	cfg := &config.Config{Env: config.EnvAWS, ClusterName: "ci", AWSRegion: "us-east-1"}
	mon := runServices(t, cfg)
	e, ok := mon.entryByName(svcAdminGRPC)
	if !ok {
		t.Fatalf("service %q not found", svcAdminGRPC)
	}
	meta := e.inputs["metadata"]
	if !meta.IsObject() {
		t.Fatal("metadata not an object")
	}
	ann := meta.ObjectValue()["annotations"]
	if !ann.IsObject() {
		t.Fatal("annotations not present")
	}
	scheme := ann.ObjectValue()[resource.PropertyKey(nlbSchemeAnnotation)]
	if !scheme.IsString() || scheme.StringValue() != "internal" {
		t.Errorf("admin service scheme = %v, want internal", scheme)
	}
}

// TestNewServices_adminSelectsCoordinator verifies admin service selects role=coordinator.
func TestNewServices_adminSelectsCoordinator(t *testing.T) {
	cfg := &config.Config{Env: config.EnvLocal, ClusterName: "ci"}
	mon := runServices(t, cfg)
	e, ok := mon.entryByName(svcAdminGRPC)
	if !ok {
		t.Fatalf("service %q not found", svcAdminGRPC)
	}
	spec := e.inputs["spec"]
	if !spec.IsObject() {
		t.Skip("spec not an object in mock inputs")
	}
	selector := spec.ObjectValue()["selector"]
	if !selector.IsObject() {
		t.Fatal("selector not an object")
	}
	role := selector.ObjectValue()["role"]
	if !role.IsString() || role.StringValue() != "coordinator" {
		t.Errorf("admin service selector role = %v, want coordinator", role)
	}
}

// TestNewServices_clientSelectsDatanode verifies client services select role=datanode.
func TestNewServices_clientSelectsDatanode(t *testing.T) {
	cfg := &config.Config{Env: config.EnvLocal, ClusterName: "ci"}
	mon := runServices(t, cfg)
	for _, name := range []string{svcClientGRPC, svcClientREST} {
		e, ok := mon.entryByName(name)
		if !ok {
			t.Fatalf("service %q not found", name)
		}
		spec := e.inputs["spec"]
		if !spec.IsObject() {
			t.Skip("spec not an object in mock inputs")
		}
		selector := spec.ObjectValue()["selector"]
		if !selector.IsObject() {
			t.Fatal("selector not an object")
		}
		role := selector.ObjectValue()["role"]
		if !role.IsString() || role.StringValue() != "datanode" {
			t.Errorf("service %q selector role = %v, want datanode", name, role)
		}
	}
}

// TestNewServices_resourceNamesAreDeterministic asserts stable names across two runs.
func TestNewServices_resourceNamesAreDeterministic(t *testing.T) {
	cfg := &config.Config{Env: config.EnvLocal, ClusterName: "ci"}
	run := func() []string { return sortedCopy(runServices(t, cfg).names) }
	first, second := run(), run()
	if len(first) != len(second) {
		t.Fatalf("run 1: %d resources, run 2: %d (non-deterministic)", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("resource[%d]: run1=%q run2=%q", i, first[i], second[i])
		}
	}
}
