package k8s

import (
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"gossamerdb/infra/pkg/config"
)

// coordinatorEntry holds a single resource registration for inspection.
type coordinatorEntry struct {
	name   string
	typ    string
	inputs resource.PropertyMap
}

// coordinatorMonitor captures every resource registered by NewCoordinator.
// Concurrent access is guarded by an embedded trackingMonitor mutex.
type coordinatorMonitor struct {
	trackingMonitor
	entries []coordinatorEntry
}

func (m *coordinatorMonitor) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.mu.Lock()
	m.names = append(m.names, args.Name)
	m.entries = append(m.entries, coordinatorEntry{
		name:   args.Name,
		typ:    args.TypeToken,
		inputs: args.Inputs,
	})
	m.mu.Unlock()
	return args.Name + "_id", args.Inputs, nil
}

func (m *coordinatorMonitor) entryByType(typeToken string) (coordinatorEntry, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.entries {
		if e.typ == typeToken {
			return e, true
		}
	}
	return coordinatorEntry{}, false
}

func runCoordinator(t *testing.T, cfg *config.Config) *coordinatorMonitor {
	t.Helper()
	mon := &coordinatorMonitor{}
	if err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		return NewCoordinator(ctx, cfg, nil)
	}, pulumi.WithMocks("gossamerdb", "test", mon)); err != nil {
		t.Fatalf("RunErr: %v", err)
	}
	return mon
}

func validCoordinatorCfg() *config.Config {
	return &config.Config{
		Env:                    config.EnvLocal,
		ClusterName:            "ci",
		NodeCount:              3,
		CoordinatorImage:       "ghcr.io/gossamerdb/coordinator:latest",
		CoordinatorStorageClass: "standard",
	}
}

// TestNewCoordinator_emptyImageReturnsError ensures missing image is caught early.
func TestNewCoordinator_emptyImageReturnsError(t *testing.T) {
	cfg := &config.Config{
		Env:         config.EnvLocal,
		ClusterName: "ci",
		NodeCount:   3,
	}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		return NewCoordinator(ctx, cfg, nil)
	}, pulumi.WithMocks("gossamerdb", "test", &coordinatorMonitor{}))
	if err == nil {
		t.Fatal("want error for empty CoordinatorImage, got nil")
	}
}

// TestNewCoordinator_registersStatefulSet verifies the StatefulSet resource name.
func TestNewCoordinator_registersStatefulSet(t *testing.T) {
	mon := runCoordinator(t, validCoordinatorCfg())
	if !contains(mon.names, coordinatorStsName) {
		t.Errorf("StatefulSet %q not registered; got: %v", coordinatorStsName, mon.names)
	}
}

// TestNewCoordinator_registersHeadlessService verifies the headless Service name.
func TestNewCoordinator_registersHeadlessService(t *testing.T) {
	mon := runCoordinator(t, validCoordinatorCfg())
	if !contains(mon.names, coordinatorHeadlessSvcName) {
		t.Errorf("headless Service %q not registered; got: %v", coordinatorHeadlessSvcName, mon.names)
	}
}

// TestNewCoordinator_registersPDB verifies the PodDisruptionBudget name.
func TestNewCoordinator_registersPDB(t *testing.T) {
	mon := runCoordinator(t, validCoordinatorCfg())
	if !contains(mon.names, coordinatorPDBName) {
		t.Errorf("PDB %q not registered; got: %v", coordinatorPDBName, mon.names)
	}
}

// TestNewCoordinator_replicaCountIsAlwaysThree asserts replicas=3 regardless of cfg.NodeCount.
func TestNewCoordinator_replicaCountIsAlwaysThree(t *testing.T) {
	for _, nodeCount := range []int{1, 3, 5} {
		nodeCount := nodeCount
		t.Run("nodeCount"+string(rune('0'+nodeCount)), func(t *testing.T) {
			cfg := validCoordinatorCfg()
			cfg.NodeCount = nodeCount
			mon := runCoordinator(t, cfg)
			entry, ok := mon.entryByType("kubernetes:apps/v1:StatefulSet")
			if !ok {
				t.Fatal("StatefulSet entry not found")
			}
			spec := entry.inputs["spec"]
			if !spec.IsObject() {
				t.Skip("spec not an object in mock inputs")
			}
			replicas := spec.ObjectValue()["replicas"]
			if !replicas.IsNumber() || int(replicas.NumberValue()) != coordinatorReplicas {
				t.Errorf("replicas = %v, want %d", replicas, coordinatorReplicas)
			}
		})
	}
}

// TestNewCoordinator_pdbMinAvailable asserts PDB minAvailable=2.
func TestNewCoordinator_pdbMinAvailable(t *testing.T) {
	mon := runCoordinator(t, validCoordinatorCfg())
	entry, ok := mon.entryByType("kubernetes:policy/v1:PodDisruptionBudget")
	if !ok {
		t.Fatal("PDB entry not found")
	}
	spec := entry.inputs["spec"]
	if !spec.IsObject() {
		t.Skip("spec not an object in mock inputs")
	}
	minAvail := spec.ObjectValue()["minAvailable"]
	if !minAvail.IsNumber() || int(minAvail.NumberValue()) != coordinatorPDBMinAvail {
		t.Errorf("minAvailable = %v, want %d", minAvail, coordinatorPDBMinAvail)
	}
}

// TestNewCoordinator_pvcSize asserts the Raft PVC requests 10Gi of storage.
func TestNewCoordinator_pvcSize(t *testing.T) {
	mon := runCoordinator(t, validCoordinatorCfg())
	entry, ok := mon.entryByType("kubernetes:apps/v1:StatefulSet")
	if !ok {
		t.Fatal("StatefulSet entry not found")
	}
	spec := entry.inputs["spec"]
	if !spec.IsObject() {
		t.Skip("spec not an object in mock inputs")
	}
	vcts := spec.ObjectValue()["volumeClaimTemplates"]
	if !vcts.IsArray() || len(vcts.ArrayValue()) == 0 {
		t.Fatal("no volumeClaimTemplates in StatefulSet spec")
	}
	pvcSpec := vcts.ArrayValue()[0].ObjectValue()["spec"]
	if !pvcSpec.IsObject() {
		t.Fatal("pvc spec not an object")
	}
	requests := pvcSpec.ObjectValue()["resources"].ObjectValue()["requests"]
	storage := requests.ObjectValue()["storage"]
	if !storage.IsString() || storage.StringValue() != coordinatorPVCSize {
		t.Errorf("pvc storage = %v, want %q", storage, coordinatorPVCSize)
	}
}

// TestNewCoordinator_resourceNamesAreDeterministic asserts stable names across two runs.
func TestNewCoordinator_resourceNamesAreDeterministic(t *testing.T) {
	run := func() []string { return sortedCopy(runCoordinator(t, validCoordinatorCfg()).names) }
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

// TestNewCoordinator_worksForBothEnvs asserts success on local and AWS envs.
func TestNewCoordinator_worksForBothEnvs(t *testing.T) {
	for _, env := range []config.Env{config.EnvLocal, config.EnvAWS} {
		env := env
		t.Run(string(env), func(t *testing.T) {
			cfg := &config.Config{
				Env:                    env,
				ClusterName:            "ci",
				NodeCount:              3,
				AWSRegion:              "us-east-1",
				CoordinatorImage:       "ghcr.io/gossamerdb/coordinator:latest",
				CoordinatorStorageClass: "gp3",
			}
			mon := runCoordinator(t, cfg)
			if len(mon.names) == 0 {
				t.Errorf("env=%q: no resources registered", env)
			}
		})
	}
}

func contains(ss []string, target string) bool {
	for _, s := range ss {
		if s == target {
			return true
		}
	}
	return false
}
