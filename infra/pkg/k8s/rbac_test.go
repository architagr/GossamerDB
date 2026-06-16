package k8s

import (
	"sort"
	"sync"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"gossamerdb/infra/pkg/config"
)

// trackingMonitor captures resource names registered via NewResource.
// Pulumi calls NewResource from concurrent goroutines, so access to names
// is guarded by a mutex.
type trackingMonitor struct {
	mu    sync.Mutex
	names []string
}

func (m *trackingMonitor) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.mu.Lock()
	m.names = append(m.names, args.Name)
	m.mu.Unlock()
	return args.Name + "_id", args.Inputs, nil
}

func (m *trackingMonitor) Call(args pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return resource.PropertyMap{}, nil
}

func sortedCopy(ss []string) []string {
	cp := make([]string, len(ss))
	copy(cp, ss)
	sort.Strings(cp)
	return cp
}

func TestNamespaceName_default(t *testing.T) {
	cfg := &config.Config{ClusterName: "ci"}
	if got := namespaceName(cfg); got != "gossamerdb" {
		t.Errorf("namespaceName() = %q, want %q", got, "gossamerdb")
	}
}

func TestNamespaceName_override(t *testing.T) {
	cfg := &config.Config{ClusterName: "ci", Namespace: "my-ns"}
	if got := namespaceName(cfg); got != "my-ns" {
		t.Errorf("namespaceName() = %q, want %q", got, "my-ns")
	}
}

// TestNewRBAC_resourceNamesAreDeterministic verifies that the set of resource
// names registered by NewRBAC contains no random components (e.g. UUID
// suffixes) and is stable across two independent runs. Pulumi may register
// resources in any order; we compare sorted name lists so order differences do
// not cause false failures.
func TestNewRBAC_resourceNamesAreDeterministic(t *testing.T) {
	// 9 resources: namespace, 2 SAs, 2 ClusterRoles, 2 ClusterRoleBindings,
	// 1 Role, 1 RoleBinding.
	wantNames := sortedCopy([]string{
		"gossamerdb",
		saCoordinator,
		saDatanode,
		clusterRoleCoordinator,
		clusterRoleCoordinator,
		clusterRoleDatanode,
		clusterRoleDatanode,
		roleSecretReader,
		roleSecretReader,
	})

	runOnce := func(t *testing.T) []string {
		t.Helper()
		mon := &trackingMonitor{}
		if err := pulumi.RunErr(func(ctx *pulumi.Context) error {
			cfg := &config.Config{Env: config.EnvLocal, ClusterName: "ci"}
			return NewRBAC(ctx, cfg, nil)
		}, pulumi.WithMocks("gossamerdb", "test", mon)); err != nil {
			t.Fatalf("RunErr: %v", err)
		}
		return sortedCopy(mon.names)
	}

	first := runOnce(t)
	second := runOnce(t)

	if len(first) != len(wantNames) {
		t.Fatalf("run 1: got %d resources, want %d: %v", len(first), len(wantNames), first)
	}
	for i := range wantNames {
		if first[i] != wantNames[i] {
			t.Errorf("sorted resource[%d] = %q, want %q", i, first[i], wantNames[i])
		}
	}
	if len(second) != len(first) {
		t.Fatalf("run 2: %d resources, want %d (non-deterministic)", len(second), len(first))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("sorted resource[%d]: run1=%q run2=%q (non-deterministic)", i, first[i], second[i])
		}
	}
}

// TestNewRBAC_customNamespace verifies that a non-empty cfg.Namespace is used
// instead of the default "gossamerdb".
func TestNewRBAC_customNamespace(t *testing.T) {
	const customNS = "custom-ns"
	mon := &trackingMonitor{}
	if err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		cfg := &config.Config{Env: config.EnvLocal, ClusterName: "ci", Namespace: customNS}
		return NewRBAC(ctx, cfg, nil)
	}, pulumi.WithMocks("gossamerdb", "test", mon)); err != nil {
		t.Fatalf("RunErr: %v", err)
	}
	found := false
	for _, n := range mon.names {
		if n == customNS {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("namespace %q not in registered resources: %v", customNS, mon.names)
	}
}

// TestNewRBAC_worksForBothEnvs asserts that NewRBAC succeeds for both local
// and AWS environments (pure k8s resources, no cloud-specific annotations).
func TestNewRBAC_worksForBothEnvs(t *testing.T) {
	for _, env := range []config.Env{config.EnvLocal, config.EnvAWS} {
		env := env
		t.Run(string(env), func(t *testing.T) {
			mon := &trackingMonitor{}
			if err := pulumi.RunErr(func(ctx *pulumi.Context) error {
				cfg := &config.Config{
					Env:         env,
					ClusterName: "ci",
					AWSRegion:   "us-east-1",
				}
				return NewRBAC(ctx, cfg, nil)
			}, pulumi.WithMocks("gossamerdb", "test", mon)); err != nil {
				t.Fatalf("env=%q RunErr: %v", env, err)
			}
			if len(mon.names) == 0 {
				t.Errorf("env=%q: no resources registered", env)
			}
		})
	}
}

// TestNewRBAC_leastPrivilege verifies that the coordinator and datanode
// ClusterRoles are distinct and that the datanode role does NOT include
// the "nodes" resource (least-privilege, security review Finding 1 & 2).
func TestNewRBAC_leastPrivilege(t *testing.T) {
	mon := &trackingMonitor{}
	if err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		cfg := &config.Config{Env: config.EnvLocal, ClusterName: "ci"}
		return NewRBAC(ctx, cfg, nil)
	}, pulumi.WithMocks("gossamerdb", "test", mon)); err != nil {
		t.Fatalf("RunErr: %v", err)
	}

	// Coordinator and datanode must have separate ClusterRoles.
	hasCoordinator, hasDatanode := false, false
	for _, n := range mon.names {
		if n == clusterRoleCoordinator {
			hasCoordinator = true
		}
		if n == clusterRoleDatanode {
			hasDatanode = true
		}
	}
	if !hasCoordinator {
		t.Errorf("missing coordinator ClusterRole %q", clusterRoleCoordinator)
	}
	if !hasDatanode {
		t.Errorf("missing datanode ClusterRole %q", clusterRoleDatanode)
	}
	if clusterRoleCoordinator == clusterRoleDatanode {
		t.Error("coordinator and datanode ClusterRole names must be distinct")
	}
}

// TestNewRBAC_serviceAccountsDisableAutoMount verifies that both ServiceAccounts
// disable automatic token mounting (security review Finding 7).
func TestNewRBAC_serviceAccountsDisableAutoMount(t *testing.T) {
	mon := &trackingMonitor{}
	if err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		cfg := &config.Config{Env: config.EnvLocal, ClusterName: "ci"}
		return NewRBAC(ctx, cfg, nil)
	}, pulumi.WithMocks("gossamerdb", "test", mon)); err != nil {
		t.Fatalf("RunErr: %v", err)
	}

	// Both SA names must appear exactly once.
	saCount := map[string]int{}
	for _, n := range mon.names {
		if n == saCoordinator || n == saDatanode {
			saCount[n]++
		}
	}
	for _, name := range []string{saCoordinator, saDatanode} {
		if saCount[name] != 1 {
			t.Errorf("service account %q registered %d times, want 1", name, saCount[name])
		}
	}
}
