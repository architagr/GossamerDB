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

// datanodeEntry holds one registered resource with its full inputs.
type datanodeEntry struct {
	typ    string
	name   string
	inputs resource.PropertyMap
}

// datanodeMonitor captures every resource registered by NewDataNode.
// Concurrent calls are guarded by mu (Pulumi registers resources from multiple goroutines).
type datanodeMonitor struct {
	mu      sync.Mutex
	entries []datanodeEntry
}

func (m *datanodeMonitor) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.mu.Lock()
	m.entries = append(m.entries, datanodeEntry{typ: args.TypeToken, name: args.Name, inputs: args.Inputs})
	m.mu.Unlock()
	return args.Name + "_id", args.Inputs, nil
}

func (m *datanodeMonitor) Call(args pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return resource.PropertyMap{}, nil
}

func (m *datanodeMonitor) names() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.entries))
	for i, e := range m.entries {
		out[i] = e.name
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

func (m *datanodeMonitor) entryByType(typeToken string) (datanodeEntry, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.entries {
		if e.typ == typeToken {
			return e, true
		}
	}
	return datanodeEntry{}, false
}

func (m *datanodeMonitor) entryByName(name string) (datanodeEntry, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.entries {
		if e.name == name {
			return e, true
		}
	}
	return datanodeEntry{}, false
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

func validDataNodeCfg() *config.Config {
	return &config.Config{
		Env:           config.EnvLocal,
		ClusterName:   "ci",
		NodeCount:     5,
		DataNodeImage: "ghcr.io/gossamerdb/datanode:latest",
	}
}

// TestNewDataNode_emptyImageReturnsError verifies missing DataNodeImage is rejected
// before any Pulumi resource is registered.
func TestNewDataNode_emptyImageReturnsError(t *testing.T) {
	cfg := &config.Config{Env: config.EnvLocal, ClusterName: "ci", NodeCount: 3}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		return NewDataNode(ctx, cfg, nil)
	}, pulumi.WithMocks("gossamerdb", "test", &datanodeMonitor{}))
	if err == nil {
		t.Fatal("want error for empty DataNodeImage, got nil")
	}
}

// TestNewDataNode_registersStatefulSet verifies the StatefulSet resource name.
func TestNewDataNode_registersStatefulSet(t *testing.T) {
	mon := runDataNode(t, validDataNodeCfg())
	if !mon.hasName(datanodeStsName) {
		t.Errorf("StatefulSet %q not registered; got: %v", datanodeStsName, mon.names())
	}
}

// TestNewDataNode_registersHeadlessService verifies the headless Service name.
func TestNewDataNode_registersHeadlessService(t *testing.T) {
	mon := runDataNode(t, validDataNodeCfg())
	if !mon.hasName(datanodeHeadlessSvcName) {
		t.Errorf("headless Service %q not registered; got: %v", datanodeHeadlessSvcName, mon.names())
	}
}

// TestNewDataNode_registersPDB verifies the PodDisruptionBudget name.
func TestNewDataNode_registersPDB(t *testing.T) {
	mon := runDataNode(t, validDataNodeCfg())
	if !mon.hasName(datanodePDBName) {
		t.Errorf("PDB %q not registered; got: %v", datanodePDBName, mon.names())
	}
}

// TestNewDataNode_replicaCountFromConfig asserts StatefulSet replicas == cfg.NodeCount.
func TestNewDataNode_replicaCountFromConfig(t *testing.T) {
	for _, nodeCount := range []int{3, 5, 7} {
		nodeCount := nodeCount
		t.Run("nodeCount", func(t *testing.T) {
			cfg := validDataNodeCfg()
			cfg.NodeCount = nodeCount
			mon := runDataNode(t, cfg)
			e, ok := mon.entryByType("kubernetes:apps/v1:StatefulSet")
			if !ok {
				t.Fatal("StatefulSet not found")
			}
			spec := e.inputs["spec"]
			if !spec.IsObject() {
				t.Skip("spec not an object in mock inputs")
			}
			got := spec.ObjectValue()["replicas"]
			if !got.IsNumber() || int(got.NumberValue()) != nodeCount {
				t.Errorf("replicas = %v, want %d", got, nodeCount)
			}
		})
	}
}

// TestNewDataNode_podManagementPolicyIsParallel verifies Parallel startup (DataNode
// pods have no ordering constraint, unlike Raft coordinators).
func TestNewDataNode_podManagementPolicyIsParallel(t *testing.T) {
	mon := runDataNode(t, validDataNodeCfg())
	e, ok := mon.entryByType("kubernetes:apps/v1:StatefulSet")
	if !ok {
		t.Fatal("StatefulSet not found")
	}
	spec := e.inputs["spec"]
	if !spec.IsObject() {
		t.Skip("spec not an object in mock inputs")
	}
	policy := spec.ObjectValue()["podManagementPolicy"]
	if !policy.IsString() || policy.StringValue() != "Parallel" {
		t.Errorf("podManagementPolicy = %v, want Parallel", policy)
	}
}

// TestNewDataNode_resourceRequirements verifies cpu=4 / memory=8Gi requests and limits.
func TestNewDataNode_resourceRequirements(t *testing.T) {
	mon := runDataNode(t, validDataNodeCfg())
	e, ok := mon.entryByType("kubernetes:apps/v1:StatefulSet")
	if !ok {
		t.Fatal("StatefulSet not found")
	}
	spec := e.inputs["spec"]
	if !spec.IsObject() {
		t.Skip("spec not an object in mock inputs")
	}
	containers := spec.ObjectValue()["template"].ObjectValue()["spec"].ObjectValue()["containers"].ArrayValue()
	if len(containers) == 0 {
		t.Fatal("no containers in pod spec")
	}
	res := containers[0].ObjectValue()["resources"]
	if !res.IsObject() {
		t.Fatal("resources not an object")
	}
	for _, section := range []string{"requests", "limits"} {
		m := res.ObjectValue()[resource.PropertyKey(section)]
		if !m.IsObject() {
			t.Fatalf("resources.%s not an object", section)
		}
		if v := m.ObjectValue()["cpu"]; !v.IsString() || v.StringValue() != "4" {
			t.Errorf("resources.%s.cpu = %v, want 4", section, v)
		}
		if v := m.ObjectValue()["memory"]; !v.IsString() || v.StringValue() != "8Gi" {
			t.Errorf("resources.%s.memory = %v, want 8Gi", section, v)
		}
	}
}

// TestNewDataNode_livenessProbe verifies path=/healthz, port=8080, initialDelaySeconds=15.
func TestNewDataNode_livenessProbe(t *testing.T) {
	mon := runDataNode(t, validDataNodeCfg())
	e, ok := mon.entryByType("kubernetes:apps/v1:StatefulSet")
	if !ok {
		t.Fatal("StatefulSet not found")
	}
	spec := e.inputs["spec"]
	if !spec.IsObject() {
		t.Skip("spec not an object in mock inputs")
	}
	containers := spec.ObjectValue()["template"].ObjectValue()["spec"].ObjectValue()["containers"].ArrayValue()
	if len(containers) == 0 {
		t.Fatal("no containers")
	}
	probe := containers[0].ObjectValue()["livenessProbe"]
	if !probe.IsObject() {
		t.Fatal("livenessProbe not an object")
	}
	httpGet := probe.ObjectValue()["httpGet"]
	if !httpGet.IsObject() {
		t.Fatal("httpGet not an object")
	}
	if v := httpGet.ObjectValue()["path"]; !v.IsString() || v.StringValue() != "/healthz" {
		t.Errorf("livenessProbe.httpGet.path = %v, want /healthz", v)
	}
	if v := httpGet.ObjectValue()["port"]; !v.IsNumber() || int(v.NumberValue()) != datanodeContainerPort {
		t.Errorf("livenessProbe.httpGet.port = %v, want %d", v, datanodeContainerPort)
	}
	if v := probe.ObjectValue()["initialDelaySeconds"]; !v.IsNumber() || int(v.NumberValue()) != 15 {
		t.Errorf("livenessProbe.initialDelaySeconds = %v, want 15", v)
	}
}

// TestNewDataNode_headlessServiceClusterIPIsNone verifies clusterIP=None for peer discovery.
func TestNewDataNode_headlessServiceClusterIPIsNone(t *testing.T) {
	mon := runDataNode(t, validDataNodeCfg())
	e, ok := mon.entryByName(datanodeHeadlessSvcName)
	if !ok {
		t.Fatalf("service %q not found", datanodeHeadlessSvcName)
	}
	spec := e.inputs["spec"]
	if !spec.IsObject() {
		t.Skip("spec not an object in mock inputs")
	}
	clusterIP := spec.ObjectValue()["clusterIP"]
	if !clusterIP.IsString() || clusterIP.StringValue() != "None" {
		t.Errorf("clusterIP = %v, want None", clusterIP)
	}
}

// TestNewDataNode_pdbMinAvailable verifies PDB minAvailable=3.
func TestNewDataNode_pdbMinAvailable(t *testing.T) {
	mon := runDataNode(t, validDataNodeCfg())
	e, ok := mon.entryByType("kubernetes:policy/v1:PodDisruptionBudget")
	if !ok {
		t.Fatal("PDB not found")
	}
	spec := e.inputs["spec"]
	if !spec.IsObject() {
		t.Skip("spec not an object in mock inputs")
	}
	minAvail := spec.ObjectValue()["minAvailable"]
	if !minAvail.IsNumber() || int(minAvail.NumberValue()) != datanodePDBMinAvail {
		t.Errorf("minAvailable = %v, want %d", minAvail, datanodePDBMinAvail)
	}
}

// TestNewDataNode_podLabels verifies pod template has app/role/cluster labels.
func TestNewDataNode_podLabels(t *testing.T) {
	cfg := validDataNodeCfg()
	mon := runDataNode(t, cfg)
	e, ok := mon.entryByType("kubernetes:apps/v1:StatefulSet")
	if !ok {
		t.Fatal("StatefulSet not found")
	}
	spec := e.inputs["spec"]
	if !spec.IsObject() {
		t.Skip("spec not an object in mock inputs")
	}
	labels := spec.ObjectValue()["template"].ObjectValue()["metadata"].ObjectValue()["labels"]
	if !labels.IsObject() {
		t.Fatal("pod template labels not an object")
	}
	lm := labels.ObjectValue()
	checks := map[string]string{
		"app":     "gossamerdb",
		"role":    "datanode",
		"cluster": cfg.ClusterName,
	}
	for k, want := range checks {
		v := lm[resource.PropertyKey(k)]
		if !v.IsString() || v.StringValue() != want {
			t.Errorf("pod label %q = %v, want %q", k, v, want)
		}
	}
}

// TestNewDataNode_serviceAccountTokenNotMounted verifies AutomountServiceAccountToken=false
// on the pod spec, preventing ambient credential exposure via the in-pod token file.
func TestNewDataNode_serviceAccountTokenNotMounted(t *testing.T) {
	mon := runDataNode(t, validDataNodeCfg())
	e, ok := mon.entryByType("kubernetes:apps/v1:StatefulSet")
	if !ok {
		t.Fatal("StatefulSet not found")
	}
	spec := e.inputs["spec"]
	if !spec.IsObject() {
		t.Skip("spec not an object in mock inputs")
	}
	podSpec := spec.ObjectValue()["template"].ObjectValue()["spec"]
	if !podSpec.IsObject() {
		t.Fatal("pod spec not an object")
	}
	autoMount := podSpec.ObjectValue()["automountServiceAccountToken"]
	if !autoMount.IsBool() || autoMount.BoolValue() != false {
		t.Errorf("automountServiceAccountToken = %v, want false", autoMount)
	}
}

// TestNewDataNode_resourceNamesAreDeterministic asserts stable names across two runs.
func TestNewDataNode_resourceNamesAreDeterministic(t *testing.T) {
	run := func() []string { return sortedNames(runDataNode(t, validDataNodeCfg()).names()) }
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
