package k8s

import (
	"sort"
	"sync"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"gossamerdb/infra/pkg/config"
)

func sortedNames(names []string) []string {
	cp := make([]string, len(names))
	copy(cp, names)
	sort.Strings(cp)
	return cp
}

// datanodeMonitor captures resource type+name pairs registered during a
// NewDataNode run. Concurrent calls are guarded by a mutex (Pulumi registers
// resources from multiple goroutines).
type datanodeMonitor struct {
	mu        sync.Mutex
	resources []mockResource
}

type mockResource struct {
	typ  string
	name string
}

func (m *datanodeMonitor) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.mu.Lock()
	m.resources = append(m.resources, mockResource{typ: args.TypeToken, name: args.Name})
	m.mu.Unlock()
	return args.Name + "_id", args.Inputs, nil
}

func (m *datanodeMonitor) Call(args pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return resource.PropertyMap{}, nil
}

func (m *datanodeMonitor) names() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.resources))
	for i, r := range m.resources {
		out[i] = r.name
	}
	return out
}

func (m *datanodeMonitor) hasName(name string) bool {
	for _, n := range m.names() {
		if n == name {
			return true
		}
	}
	return false
}

func runDataNode(t *testing.T, cfg *config.Config) *datanodeMonitor {
	t.Helper()
	mon := &datanodeMonitor{}
	if err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		return NewDataNode(ctx, cfg, nil)
	}, pulumi.WithMocks("gossamerdb", "test", mon)); err != nil {
		t.Fatalf("RunErr: %v", err)
	}
	return mon
}

// TestNewDataNode_emptyImageReturnsError verifies that a missing DataNodeImage
// is rejected before any Pulumi resource is registered.
func TestNewDataNode_emptyImageReturnsError(t *testing.T) {
	cfg := &config.Config{
		Env:         config.EnvLocal,
		ClusterName: "ci",
		NodeCount:   3,
	}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		return NewDataNode(ctx, cfg, nil)
	}, pulumi.WithMocks("gossamerdb", "test", &datanodeMonitor{}))
	if err == nil {
		t.Fatal("want error for empty DataNodeImage, got nil")
	}
}

// TestNewDataNode_registersStatefulSet verifies the StatefulSet resource name.
func TestNewDataNode_registersStatefulSet(t *testing.T) {
	cfg := &config.Config{
		Env:           config.EnvLocal,
		ClusterName:   "ci",
		NodeCount:     3,
		DataNodeImage: "ghcr.io/gossamerdb/datanode:latest",
	}
	mon := runDataNode(t, cfg)
	if !mon.hasName(datanodeStsName) {
		t.Errorf("StatefulSet %q not registered; got: %v", datanodeStsName, mon.names())
	}
}

// TestNewDataNode_registersHeadlessService verifies the headless Service name.
func TestNewDataNode_registersHeadlessService(t *testing.T) {
	cfg := &config.Config{
		Env:           config.EnvLocal,
		ClusterName:   "ci",
		NodeCount:     3,
		DataNodeImage: "ghcr.io/gossamerdb/datanode:latest",
	}
	mon := runDataNode(t, cfg)
	if !mon.hasName(datanodeHeadlessSvcName) {
		t.Errorf("headless Service %q not registered; got: %v", datanodeHeadlessSvcName, mon.names())
	}
}

// TestNewDataNode_registersPDB verifies the PodDisruptionBudget name.
func TestNewDataNode_registersPDB(t *testing.T) {
	cfg := &config.Config{
		Env:           config.EnvLocal,
		ClusterName:   "ci",
		NodeCount:     5,
		DataNodeImage: "ghcr.io/gossamerdb/datanode:latest",
	}
	mon := runDataNode(t, cfg)
	if !mon.hasName(datanodePDBName) {
		t.Errorf("PDB %q not registered; got: %v", datanodePDBName, mon.names())
	}
}

// TestNewDataNode_resourceNamesAreDeterministic asserts the resource name set
// is stable across two independent runs (no random suffixes).
func TestNewDataNode_resourceNamesAreDeterministic(t *testing.T) {
	cfg := &config.Config{
		Env:           config.EnvLocal,
		ClusterName:   "ci",
		NodeCount:     5,
		DataNodeImage: "ghcr.io/gossamerdb/datanode:latest",
	}

	run := func() []string { return sortedNames(runDataNode(t, cfg).names()) }
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

// TestNewDataNode_worksForBothEnvs asserts success for local and AWS envs.
func TestNewDataNode_worksForBothEnvs(t *testing.T) {
	for _, env := range []config.Env{config.EnvLocal, config.EnvAWS} {
		env := env
		t.Run(string(env), func(t *testing.T) {
			cfg := &config.Config{
				Env:           env,
				ClusterName:   "ci",
				NodeCount:     3,
				AWSRegion:     "us-east-1",
				DataNodeImage: "ghcr.io/gossamerdb/datanode:latest",
			}
			mon := runDataNode(t, cfg)
			if len(mon.names()) == 0 {
				t.Errorf("env=%q: no resources registered", env)
			}
		})
	}
}
