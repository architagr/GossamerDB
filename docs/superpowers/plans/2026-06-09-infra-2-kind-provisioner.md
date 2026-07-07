# INFRA-2: Local kind Cluster Provisioner — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `infra/pkg/local.NewCluster` to provision a local Kubernetes cluster via the kind Go API, wrapped as a Pulumi ComponentResource with `cluster_name` and `kubeconfig_path` outputs.

**Architecture:** Pure functions (`buildNodes`, `k8sNodeImage`, `alreadyExists`) contain all logic that can be unit-tested without Docker. `createKindCluster` wraps the kind Provider API and is integration-tested with a Docker-availability guard. `NewCluster` wraps `createKindCluster` in a `pulumi.ComponentResource` and exports Pulumi stack outputs. `cmd/main.go`'s `deployLocal` stub is replaced with the real call.

**Tech Stack:** `sigs.k8s.io/kind v0.22+` (`sigs.k8s.io/kind/pkg/cluster`, `sigs.k8s.io/kind/pkg/apis/config/v1alpha4`), `github.com/pulumi/pulumi/sdk/v3`, Go 1.25.8.

---

## File Map

| Path | Action | Responsibility |
|------|--------|----------------|
| `infra/go.mod` | Modify | Add `sigs.k8s.io/kind` direct dependency |
| `infra/pkg/local/cluster.go` | Create | `KindCluster` component, `NewCluster`, `createKindCluster`, `buildNodes`, `k8sNodeImage`, `alreadyExists` |
| `infra/pkg/local/cluster_test.go` | Create | `TestBuildNodes`, `TestK8sNodeImage`, `TestNewCluster` (Docker-guarded integration) |
| `infra/cmd/main.go` | Modify | Replace `deployLocal` stub with `local.NewCluster(ctx, &cfg)` |
| `infra/Pulumi.local.yaml` | Modify | Update `nodeCount: 3` (spec default for local) |

---

### Task 1: Create Branch + Worktree

**Files:** none (git operations only)

- [ ] **Step 1: Ensure epic branch is up to date**

```bash
git fetch origin feat/12-iac-pulumi-go
```

- [ ] **Step 2: Create worktree for INFRA-2**

```bash
git worktree add /Users/architagarwal/code/gossamerdb-infra-2 -b feat/14-infra-2-kind-provisioner origin/feat/12-iac-pulumi-go
```

Expected: `/Users/architagarwal/code/gossamerdb-infra-2` exists on branch `feat/14-infra-2-kind-provisioner`.

- [ ] **Step 3: Verify baseline**

```bash
cd /Users/architagarwal/code/gossamerdb-infra-2/infra && go build ./... && go test ./...
```

Expected: `ok  gossamerdb/infra/pkg/config` (3+ tests pass, no errors).

---

### Task 2: Add `sigs.k8s.io/kind` Dependency

**Files:**
- Modify: `infra/go.mod`

- [ ] **Step 1: Fetch kind dependency**

```bash
cd /Users/architagarwal/code/gossamerdb-infra-2/infra
go get sigs.k8s.io/kind@v0.23.0
```

Expected: `go.mod` now contains `sigs.k8s.io/kind v0.23.0` as a direct dependency.

- [ ] **Step 2: Verify existing tests still pass**

```bash
cd /Users/architagarwal/code/gossamerdb-infra-2/infra && go test ./...
```

Expected: `ok  gossamerdb/infra/pkg/config`

- [ ] **Step 3: Commit**

```bash
cd /Users/architagarwal/code/gossamerdb-infra-2
git add infra/go.mod infra/go.sum
git commit -m "chore(infra): add sigs.k8s.io/kind dependency refs #14"
```

---

### Task 3: TDD — Pure Helper Functions

**Files:**
- Create: `infra/pkg/local/cluster_test.go`
- Create: `infra/pkg/local/cluster.go` (partial — helpers only)

#### Step A: Unit tests for `buildNodes` and `k8sNodeImage`

- [ ] **Step 1: Write failing tests**

Create `/Users/architagarwal/code/gossamerdb-infra-2/infra/pkg/local/cluster_test.go`:

```go
package local

import (
	"testing"

	kindv1a4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

func TestBuildNodes_controlPlaneOnly(t *testing.T) {
	nodes := buildNodes(0)
	if len(nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(nodes))
	}
	if nodes[0].Role != kindv1a4.ControlPlaneRole {
		t.Errorf("node[0].Role = %q, want ControlPlaneRole", nodes[0].Role)
	}
}

func TestBuildNodes_withWorkers(t *testing.T) {
	nodes := buildNodes(3)
	if len(nodes) != 4 {
		t.Fatalf("want 4 nodes, got %d", len(nodes))
	}
	if nodes[0].Role != kindv1a4.ControlPlaneRole {
		t.Errorf("node[0].Role = %q, want ControlPlaneRole", nodes[0].Role)
	}
	for i, n := range nodes[1:] {
		if n.Role != kindv1a4.WorkerRole {
			t.Errorf("node[%d].Role = %q, want WorkerRole", i+1, n.Role)
		}
	}
}

func TestK8sNodeImage_bareMinor(t *testing.T) {
	got := k8sNodeImage("1.29")
	want := "kindest/node:v1.29.0"
	if got != want {
		t.Errorf("k8sNodeImage(%q) = %q, want %q", "1.29", got, want)
	}
}

func TestK8sNodeImage_patch(t *testing.T) {
	got := k8sNodeImage("1.29.2")
	want := "kindest/node:v1.29.2"
	if got != want {
		t.Errorf("k8sNodeImage(%q) = %q, want %q", "1.29.2", got, want)
	}
}

func TestK8sNodeImage_vPrefix(t *testing.T) {
	got := k8sNodeImage("v1.29.0")
	want := "kindest/node:v1.29.0"
	if got != want {
		t.Errorf("k8sNodeImage(%q) = %q, want %q", "v1.29.0", got, want)
	}
}
```

- [ ] **Step 2: Run tests — confirm they fail**

```bash
cd /Users/architagarwal/code/gossamerdb-infra-2/infra && go test ./pkg/local/...
```

Expected: FAIL — `no Go files in .../pkg/local` or `buildNodes undefined`.

- [ ] **Step 3: Implement helpers in `cluster.go`**

Create `/Users/architagarwal/code/gossamerdb-infra-2/infra/pkg/local/cluster.go` with only the pure helpers for now:

```go
package local

import (
	"strings"

	kindv1a4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// buildNodes returns a kind node list: 1 control-plane followed by nodeCount workers.
func buildNodes(nodeCount int) []kindv1a4.Node {
	nodes := make([]kindv1a4.Node, 0, 1+nodeCount)
	nodes = append(nodes, kindv1a4.Node{Role: kindv1a4.ControlPlaneRole})
	for i := 0; i < nodeCount; i++ {
		nodes = append(nodes, kindv1a4.Node{Role: kindv1a4.WorkerRole})
	}
	return nodes
}

// k8sNodeImage returns the kind node image tag for a given k8s version string.
// Accepts "1.29", "1.29.0", or "v1.29.0" — always returns "kindest/node:vX.Y.Z".
func k8sNodeImage(version string) string {
	v := strings.TrimPrefix(version, "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) == 2 {
		v = v + ".0"
	}
	return "kindest/node:v" + v
}
```

- [ ] **Step 4: Run tests — confirm they pass**

```bash
cd /Users/architagarwal/code/gossamerdb-infra-2/infra && go test ./pkg/local/...
```

Expected:
```
ok  	gossamerdb/infra/pkg/local
```

- [ ] **Step 5: Run go vet**

```bash
cd /Users/architagarwal/code/gossamerdb-infra-2/infra && go vet ./pkg/local/...
```

Expected: no output.

- [ ] **Step 6: Commit**

```bash
cd /Users/architagarwal/code/gossamerdb-infra-2
git add infra/pkg/local/cluster.go infra/pkg/local/cluster_test.go
git commit -m "feat(infra): buildNodes and k8sNodeImage helpers with tests refs #14"
```

---

### Task 4: TDD — `createKindCluster` Integration Test + Implementation

**Files:**
- Modify: `infra/pkg/local/cluster_test.go` (add `TestNewCluster`)
- Modify: `infra/pkg/local/cluster.go` (add `createKindCluster`, `alreadyExists`)

- [ ] **Step 1: Add stub `createKindCluster` so tests compile**

Append to `cluster.go` (after the helpers):

```go
// createKindCluster provisions a local kind cluster and writes its kubeconfig to
// ~/.kube/gossamerdb-<name>.yaml. Idempotent: if the cluster already exists,
// skips creation and returns the kubeconfig path. Returns the kubeconfig file path.
func createKindCluster(name string, nodeCount int, k8sVersion string) (string, error) {
	return "", fmt.Errorf("not implemented")
}
```

Also add `"fmt"` to the import block.

- [ ] **Step 2: Add `TestNewCluster` integration test to `cluster_test.go`**

Append to `cluster_test.go`:

```go
import (
	"os"
	"os/exec"
	"strings"
	"testing"

	kindv1a4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)
```

**Note:** The file already has `import ("testing"; kindv1a4 ...)` — replace the entire import block at the top of `cluster_test.go` with:

```go
package local

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	kindv1a4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)
```

Then append `TestNewCluster` to the file:

```go
func TestNewCluster(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not in PATH — skipping kind integration test")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("docker daemon not running — skipping kind integration test")
	}

	name := "gossamerdb-test-ci"
	t.Cleanup(func() {
		// Delete cluster after test regardless of outcome.
		_ = exec.Command("kind", "delete", "cluster", "--name", name).Run()
	})

	kubeconfigPath, err := createKindCluster(name, 0, "1.29")
	if err != nil {
		t.Fatalf("createKindCluster: %v", err)
	}

	// Verify kubeconfig file was written.
	data, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		t.Fatalf("kubeconfig not written to %s: %v", kubeconfigPath, err)
	}
	if !strings.Contains(string(data), name) {
		t.Errorf("kubeconfig at %s does not mention cluster name %q", kubeconfigPath, name)
	}

	// Idempotency: second call must not error.
	if _, err := createKindCluster(name, 0, "1.29"); err != nil {
		t.Fatalf("second createKindCluster (idempotency): %v", err)
	}
}
```

- [ ] **Step 3: Run tests — confirm `TestNewCluster` fails (or skips without Docker)**

```bash
cd /Users/architagarwal/code/gossamerdb-infra-2/infra && go test -v -run TestNewCluster -timeout 90s ./pkg/local/...
```

Expected with Docker available: FAIL — `createKindCluster: not implemented`
Expected without Docker: `SKIP docker not in PATH...`

- [ ] **Step 4: Implement `createKindCluster` and `alreadyExists`**

Replace the stub in `cluster.go`. Update the file so the **full** `cluster.go` is:

```go
package local

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	kindcluster "sigs.k8s.io/kind/pkg/cluster"
	kindv1a4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// buildNodes returns a kind node list: 1 control-plane followed by nodeCount workers.
func buildNodes(nodeCount int) []kindv1a4.Node {
	nodes := make([]kindv1a4.Node, 0, 1+nodeCount)
	nodes = append(nodes, kindv1a4.Node{Role: kindv1a4.ControlPlaneRole})
	for i := 0; i < nodeCount; i++ {
		nodes = append(nodes, kindv1a4.Node{Role: kindv1a4.WorkerRole})
	}
	return nodes
}

// k8sNodeImage returns the kind node image tag for a given k8s version string.
// Accepts "1.29", "1.29.0", or "v1.29.0" — always returns "kindest/node:vX.Y.Z".
func k8sNodeImage(version string) string {
	v := strings.TrimPrefix(version, "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) == 2 {
		v = v + ".0"
	}
	return "kindest/node:v" + v
}

// alreadyExists reports whether a kind cluster with the given name exists.
func alreadyExists(provider *kindcluster.Provider, name string) (bool, error) {
	clusters, err := provider.List()
	if err != nil {
		return false, fmt.Errorf("list clusters: %w", err)
	}
	for _, c := range clusters {
		if c == name {
			return true, nil
		}
	}
	return false, nil
}

// createKindCluster provisions a local kind cluster and writes its kubeconfig to
// ~/.kube/gossamerdb-<name>.yaml. Idempotent: if the cluster already exists,
// skips creation and returns the kubeconfig path.
func createKindCluster(name string, nodeCount int, k8sVersion string) (string, error) {
	provider := kindcluster.NewProvider()

	exists, err := alreadyExists(provider, name)
	if err != nil {
		return "", err
	}

	if !exists {
		kindCfg := &kindv1a4.Cluster{
			Nodes: buildNodes(nodeCount),
		}
		if err := provider.Create(
			name,
			kindcluster.CreateWithV1Alpha4Config(kindCfg),
			kindcluster.CreateWithNodeImage(k8sNodeImage(k8sVersion)),
		); err != nil {
			return "", fmt.Errorf("kind create: %w", err)
		}
	}

	kubeConfigStr, err := provider.KubeConfig(name, false)
	if err != nil {
		return "", fmt.Errorf("get kubeconfig: %w", err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}

	kubeconfigPath := filepath.Join(homeDir, ".kube", "gossamerdb-"+name+".yaml")
	if err := os.MkdirAll(filepath.Dir(kubeconfigPath), 0o755); err != nil {
		return "", fmt.Errorf("mkdir kubeconfig dir: %w", err)
	}
	if err := os.WriteFile(kubeconfigPath, []byte(kubeConfigStr), 0o600); err != nil {
		return "", fmt.Errorf("write kubeconfig: %w", err)
	}

	return kubeconfigPath, nil
}
```

- [ ] **Step 5: Build to verify it compiles**

```bash
cd /Users/architagarwal/code/gossamerdb-infra-2/infra && go build ./pkg/local/...
```

Expected: no errors.

- [ ] **Step 6: Run all tests**

```bash
cd /Users/architagarwal/code/gossamerdb-infra-2/infra && go test -v -timeout 90s ./pkg/local/...
```

Expected:
- `TestBuildNodes_*` and `TestK8sNodeImage_*` pass immediately.
- `TestNewCluster`: passes (if Docker running, creates real cluster ≤60s) OR skips (no Docker).

- [ ] **Step 7: Commit**

```bash
cd /Users/architagarwal/code/gossamerdb-infra-2
git add infra/pkg/local/cluster.go infra/pkg/local/cluster_test.go
git commit -m "feat(infra): createKindCluster with idempotency and kubeconfig export refs #14"
```

---

### Task 5: Pulumi `KindCluster` Component Resource + `NewCluster`

**Files:**
- Modify: `infra/pkg/local/cluster.go` (append KindCluster + NewCluster)

- [ ] **Step 1: Append `KindCluster` and `NewCluster` to `cluster.go`**

Append to the end of `infra/pkg/local/cluster.go`:

```go
// KindCluster is a Pulumi ComponentResource that represents a local kind cluster.
// It gives the cluster a URN in the Pulumi state file.
type KindCluster struct {
	pulumi.ResourceState

	ClusterName    pulumi.StringOutput
	KubeconfigPath pulumi.StringOutput
}

// NewCluster creates a local kind cluster as a Pulumi component and exports
// cluster_name and kubeconfig_path as stack outputs.
func NewCluster(ctx *pulumi.Context, cfg *config.Config) error {
	c := &KindCluster{}
	if err := ctx.RegisterComponentResource("gossamerdb:infra:KindCluster", cfg.ClusterName, c); err != nil {
		return fmt.Errorf("register component: %w", err)
	}

	kubeconfigPath, err := createKindCluster(cfg.ClusterName, cfg.NodeCount, cfg.K8sVersion)
	if err != nil {
		return fmt.Errorf("create cluster: %w", err)
	}

	c.ClusterName = pulumi.String(cfg.ClusterName).ToStringOutput()
	c.KubeconfigPath = pulumi.String(kubeconfigPath).ToStringOutput()

	if err := ctx.RegisterResourceOutputs(c, pulumi.Map{
		"cluster_name":    pulumi.String(cfg.ClusterName),
		"kubeconfig_path": pulumi.String(kubeconfigPath),
	}); err != nil {
		return fmt.Errorf("register outputs: %w", err)
	}

	ctx.Export("cluster_name", c.ClusterName)
	ctx.Export("kubeconfig_path", c.KubeconfigPath)
	return nil
}
```

Also add these imports to `cluster.go` (the file needs two new imports for Pulumi):
- `"github.com/pulumi/pulumi/sdk/v3/go/pulumi"`
- `"gossamerdb/infra/pkg/config"`

The final import block in `cluster.go` must be:

```go
import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"gossamerdb/infra/pkg/config"
	kindcluster "sigs.k8s.io/kind/pkg/cluster"
	kindv1a4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)
```

- [ ] **Step 2: Build to verify**

```bash
cd /Users/architagarwal/code/gossamerdb-infra-2/infra && go build ./pkg/local/...
```

Expected: no errors.

- [ ] **Step 3: Run all tests (must still pass)**

```bash
cd /Users/architagarwal/code/gossamerdb-infra-2/infra && go test -timeout 90s ./pkg/local/...
```

Expected: `ok  gossamerdb/infra/pkg/local`

- [ ] **Step 4: Commit**

```bash
cd /Users/architagarwal/code/gossamerdb-infra-2
git add infra/pkg/local/cluster.go
git commit -m "feat(infra): KindCluster Pulumi component + NewCluster entry point refs #14"
```

---

### Task 6: Wire into `cmd/main.go` + Update `Pulumi.local.yaml`

**Files:**
- Modify: `infra/cmd/main.go` (lines ~41-44: replace `deployLocal` stub)
- Modify: `infra/Pulumi.local.yaml` (update `nodeCount: 3`)

- [ ] **Step 1: Update `deployLocal` in `cmd/main.go`**

Read `infra/cmd/main.go`. Replace the `deployLocal` function:

```go
// deployLocal provisions a kind cluster for local development via INFRA-2.
func deployLocal(ctx *pulumi.Context, cfg config.Config) error {
	return local.NewCluster(ctx, &cfg)
}
```

Also add import `"gossamerdb/infra/pkg/local"` to the import block. Remove the now-unused `"fmt"` from the import if it's no longer used by other functions (check: `deployK8s` and `deployAWS` stubs still use `fmt.Sprintf`, so keep `"fmt"`).

Final import block in `cmd/main.go`:

```go
import (
	"fmt"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"gossamerdb/infra/pkg/config"
	"gossamerdb/infra/pkg/local"
)
```

- [ ] **Step 2: Update `Pulumi.local.yaml` worker count**

Read `infra/Pulumi.local.yaml`. Update `nodeCount` from `1` to `3`:

```yaml
config:
  gossamerdb-infra:env: local
  gossamerdb-infra:clusterName: gossamerdb-local
  gossamerdb-infra:k8sVersion: "1.29"
  gossamerdb-infra:nodeCount: 3
  gossamerdb-infra:namespace: gossamerdb
```

- [ ] **Step 3: Build entire infra module**

```bash
cd /Users/architagarwal/code/gossamerdb-infra-2/infra && go build ./...
```

Expected: no errors.

- [ ] **Step 4: Run all tests**

```bash
cd /Users/architagarwal/code/gossamerdb-infra-2/infra && go test -timeout 90s ./...
```

Expected: `ok  gossamerdb/infra/pkg/config`, `ok  gossamerdb/infra/pkg/local`

- [ ] **Step 5: Commit**

```bash
cd /Users/architagarwal/code/gossamerdb-infra-2
git add infra/cmd/main.go infra/Pulumi.local.yaml
git commit -m "feat(infra): wire NewCluster into deployLocal, update local nodeCount to 3 refs #14"
```

---

### Task 7: Final Verification + Draft PR

- [ ] **Step 1: Full build + vet + test + lint**

```bash
cd /Users/architagarwal/code/gossamerdb-infra-2/infra
go build ./...
go vet ./...
go test -timeout 90s ./...
golangci-lint run ./...
```

Expected: all clean, 0 lint issues.

- [ ] **Step 2: Verify acceptance criteria**

Check manually:
- `infra/pkg/local/cluster.go` contains `NewCluster(ctx *pulumi.Context, cfg *config.Config) error` ✓
- Kind Go API used (no `os/exec` for kind operations) ✓
- Cluster name = `cfg.ClusterName`, k8s version from `cfg.K8sVersion` ✓
- Writes kubeconfig to `~/.kube/gossamerdb-<ClusterName>.yaml` ✓
- Pulumi outputs `cluster_name` and `kubeconfig_path` ✓
- `TestNewCluster` in test file ✓

- [ ] **Step 3: Push and open draft PR**

```bash
cd /Users/architagarwal/code/gossamerdb-infra-2
git push -u origin feat/14-infra-2-kind-provisioner
gh pr create \
  --title "feat(infra): INFRA-2 local kind cluster provisioner (Pulumi Go)" \
  --body "$(cat <<'EOF'
## Summary

- Adds `infra/pkg/local.NewCluster` — provisions a kind cluster as a Pulumi ComponentResource
- Uses `sigs.k8s.io/kind/pkg/cluster` Go API directly (no shell-out to CLI)
- Idempotent: `alreadyExists` check via `provider.List()` before creation
- Kubeconfig written to `~/.kube/gossamerdb-<name>.yaml` after cluster ready
- Pulumi outputs: `cluster_name`, `kubeconfig_path`
- Pure helpers (`buildNodes`, `k8sNodeImage`) are unit-tested; integration test skips without Docker
- Updates `Pulumi.local.yaml` nodeCount to 3 (spec default for local)
- Wires `deployLocal` in `cmd/main.go` to the real provisioner

Fixes #14

## Test Plan

- [x] `TestBuildNodes_*` — pass without Docker
- [x] `TestK8sNodeImage_*` — pass without Docker
- [x] `TestNewCluster` — passes (Docker) or skips (no Docker)
- [x] `go build ./...` — clean
- [x] `go vet ./...` — clean
- [x] `golangci-lint run ./...` — 0 issues

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)" \
  --base feat/12-iac-pulumi-go \
  --draft
```

---

## Acceptance Criteria Checklist (from issue #14)

- [ ] `infra/pkg/local/cluster.go` — `NewCluster(ctx *pulumi.Context, cfg *config.Config) error`
- [ ] Uses `sigs.k8s.io/kind/pkg/cluster` Go API — no `os/exec` for kind
- [ ] Cluster name = `cfg.ClusterName`; k8s version from `cfg.K8sVersion`
- [ ] 1 control-plane + `cfg.NodeCount` workers
- [ ] Kubeconfig → `~/.kube/gossamerdb-<ClusterName>.yaml`
- [ ] Pulumi outputs: `cluster_name`, `kubeconfig_path`
- [ ] `TestNewCluster` passes (or skips gracefully without Docker)
- [ ] `go vet ./...` clean
