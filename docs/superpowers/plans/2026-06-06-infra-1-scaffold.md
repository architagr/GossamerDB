# INFRA-1: IaC Project Scaffold + Config Model — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a separate `infra/` Go module with a typed `Config` struct and a Pulumi entry point that dispatches to the correct provisioner based on `--stack` / `ENV`.

**Architecture:** `infra/` is an isolated Go module (`gossamerdb/infra`) so Pulumi SDK deps never bleed into the core binary. Config loading is split into two files — `config.go` (pure Go struct + `Validate()`, zero Pulumi imports, fully unit-testable) and `pulumi.go` (`Load(ctx)` wraps `pulumi/config`). `cmd/main.go` calls `config.Load` then switches on `Env`.

**Tech Stack:** `github.com/pulumi/pulumi/sdk/v3`, Go 1.21+, Pulumi CLI (for `pulumi up/preview/destroy`), GNU Make.

---

## File Map

| Path | Action | Responsibility |
|------|--------|----------------|
| `infra/go.mod` | Create | Module declaration `gossamerdb/infra` |
| `infra/pkg/config/config.go` | Create | `Config` struct, `Env` enum, `Validate()` |
| `infra/pkg/config/pulumi.go` | Create | `Load(*pulumi.Context) (Config, error)` |
| `infra/pkg/config/config_test.go` | Create | Unit tests for `Validate()` (no Pulumi dep) |
| `infra/cmd/main.go` | Create | `pulumi.Run` entry point, dispatch switch |
| `infra/Pulumi.yaml` | Create | Pulumi project file (runtime: go) |
| `infra/Pulumi.local.yaml` | Create | Stack config for `local` env |
| `infra/README.md` | Create | Prereqs, stack init, run commands, adding envs |
| `makefile` | Modify | Add `infra-up`, `infra-down`, `infra-preview` targets |

---

### Task 1: Create Git Branches

**Files:** none (git operations only)

- [ ] **Step 1: Create `develop` branch from `main`**

```bash
git checkout main
git checkout -b develop
git push -u origin develop
```

Expected: branch `develop` exists locally and on `origin`.

- [ ] **Step 2: Create epic feature branch from `develop`**

```bash
git checkout develop
git checkout -b feat/12-iac-pulumi-go
git push -u origin feat/12-iac-pulumi-go
```

Expected: `feat/12-iac-pulumi-go` exists on `origin`.

- [ ] **Step 3: Create story sub-branch**

```bash
git checkout feat/12-iac-pulumi-go
git checkout -b feat/12-iac-pulumi-go/infra-1-scaffold
```

Expected: now on `feat/12-iac-pulumi-go/infra-1-scaffold`.

---

### Task 2: Bootstrap the `infra/` Go Module

**Files:**
- Create: `infra/go.mod`
- Create: `infra/Pulumi.yaml`
- Create: `infra/Pulumi.local.yaml`

- [ ] **Step 1: Init module and fetch Pulumi SDK**

```bash
mkdir -p infra/cmd infra/pkg/config
cd infra
go mod init gossamerdb/infra
go get github.com/pulumi/pulumi/sdk/v3
```

Expected: `infra/go.mod` and `infra/go.sum` exist; `go.mod` requires `github.com/pulumi/pulumi/sdk/v3`.

- [ ] **Step 2: Create `infra/Pulumi.yaml`**

```yaml
name: gossamerdb-infra
runtime: go
description: GossamerDB infrastructure as code (Pulumi Go SDK)
```

- [ ] **Step 3: Create `infra/Pulumi.local.yaml`**

```yaml
config:
  gossamerdb-infra:env: local
  gossamerdb-infra:clusterName: gossamerdb-local
  gossamerdb-infra:k8sVersion: "1.29"
  gossamerdb-infra:nodeCount: 1
  gossamerdb-infra:namespace: gossamerdb
```

- [ ] **Step 4: Commit scaffold**

```bash
git add infra/go.mod infra/go.sum infra/Pulumi.yaml infra/Pulumi.local.yaml
git commit -m "chore: bootstrap infra Go module and Pulumi project files refs #13"
```

---

### Task 3: Config Struct + Validate (TDD — no Pulumi dep)

**Files:**
- Create: `infra/pkg/config/config.go`
- Create: `infra/pkg/config/config_test.go`

- [ ] **Step 1: Write failing tests**

Create `infra/pkg/config/config_test.go`:

```go
package config_test

import (
	"testing"

	"gossamerdb/infra/pkg/config"
)

func TestValidate_knownEnvs(t *testing.T) {
	envs := []config.Env{config.EnvLocal, config.EnvK8s, config.EnvAWS}
	for _, env := range envs {
		c := config.Config{Env: env, ClusterName: "test"}
		if err := c.Validate(); err != nil {
			t.Fatalf("env %q: unexpected error: %v", env, err)
		}
	}
}

func TestValidate_unknownEnv(t *testing.T) {
	c := config.Config{Env: "prod", ClusterName: "test"}
	err := c.Validate()
	if err == nil {
		t.Fatal("expected error for unknown env, got nil")
	}
}

func TestValidate_emptyClusterName(t *testing.T) {
	c := config.Config{Env: config.EnvLocal, ClusterName: ""}
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for empty ClusterName, got nil")
	}
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
cd infra && go test ./pkg/config/...
```

Expected: FAIL — `config_test.go:7:2: cannot find package "gossamerdb/infra/pkg/config"`

- [ ] **Step 3: Implement `infra/pkg/config/config.go`**

```go
package config

import "fmt"

type Env string

const (
	EnvLocal Env = "local"
	EnvK8s   Env = "k8s"
	EnvAWS   Env = "aws"
)

type Config struct {
	Env         Env
	ClusterName string
	K8sVersion  string
	NodeCount   int
	AWSRegion   string
	Namespace   string
}

func (c Config) Validate() error {
	if c.ClusterName == "" {
		return fmt.Errorf("clusterName must not be empty")
	}
	switch c.Env {
	case EnvLocal, EnvK8s, EnvAWS:
		return nil
	default:
		return fmt.Errorf("unknown env %q: must be local|k8s|aws", c.Env)
	}
}
```

- [ ] **Step 4: Run tests — confirm they pass**

```bash
cd infra && go test ./pkg/config/...
```

Expected: `ok  	gossamerdb/infra/pkg/config`

- [ ] **Step 5: Commit**

```bash
git add infra/pkg/config/config.go infra/pkg/config/config_test.go
git commit -m "feat(infra): add Config struct and Validate() with tests refs #13"
```

---

### Task 4: Pulumi Config Loader

**Files:**
- Create: `infra/pkg/config/pulumi.go`

- [ ] **Step 1: Create `infra/pkg/config/pulumi.go`**

```go
package config

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	pulumiconfig "github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func Load(ctx *pulumi.Context) (Config, error) {
	cfg := pulumiconfig.New(ctx, "")

	c := Config{
		Env:         Env(cfg.Require("env")),
		ClusterName: cfg.Require("clusterName"),
		K8sVersion:  cfg.Get("k8sVersion"),
		NodeCount:   cfg.GetInt("nodeCount"),
		AWSRegion:   cfg.Get("awsRegion"),
		Namespace:   cfg.Get("namespace"),
	}

	if c.K8sVersion == "" {
		c.K8sVersion = "1.29"
	}
	if c.NodeCount == 0 {
		c.NodeCount = 1
	}
	if c.Namespace == "" {
		c.Namespace = "gossamerdb"
	}

	return c, c.Validate()
}
```

- [ ] **Step 2: Verify package still compiles and tests still pass**

```bash
cd infra && go build ./pkg/config/... && go test ./pkg/config/...
```

Expected: no errors, `ok  	gossamerdb/infra/pkg/config`

- [ ] **Step 3: Commit**

```bash
git add infra/pkg/config/pulumi.go
git commit -m "feat(infra): add Load() wrapping pulumi.Config refs #13"
```

---

### Task 5: Pulumi Entry Point (`infra/cmd/main.go`)

**Files:**
- Create: `infra/cmd/main.go`

- [ ] **Step 1: Create `infra/cmd/main.go`**

```go
package main

import (
	"fmt"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"gossamerdb/infra/pkg/config"
)

func main() {
	pulumi.Run(deploy)
}

func deploy(ctx *pulumi.Context) error {
	cfg, err := config.Load(ctx)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	switch cfg.Env {
	case config.EnvLocal:
		return deployLocal(ctx, cfg)
	case config.EnvK8s:
		return deployK8s(ctx, cfg)
	case config.EnvAWS:
		return deployAWS(ctx, cfg)
	default:
		return fmt.Errorf("unhandled env %q", cfg.Env)
	}
}

// deployLocal is the stub for INFRA-2 (kind cluster provisioner).
func deployLocal(ctx *pulumi.Context, cfg config.Config) error {
	ctx.Log.Info(fmt.Sprintf("local stack ready: cluster=%s k8s=%s", cfg.ClusterName, cfg.K8sVersion), nil)
	return nil
}

// deployK8s is the stub for INFRA-4 (RBAC baseline on existing cluster).
func deployK8s(ctx *pulumi.Context, cfg config.Config) error {
	ctx.Log.Info(fmt.Sprintf("k8s stack ready: cluster=%s namespace=%s", cfg.ClusterName, cfg.Namespace), nil)
	return nil
}

// deployAWS is the stub for INFRA-3 (EKS cluster provisioner).
func deployAWS(ctx *pulumi.Context, cfg config.Config) error {
	ctx.Log.Info(fmt.Sprintf("aws stack ready: region=%s cluster=%s", cfg.AWSRegion, cfg.ClusterName), nil)
	return nil
}
```

- [ ] **Step 2: Build the entire infra module**

```bash
cd infra && go build ./...
```

Expected: no errors, binary artifact in working directory (Pulumi picks it up automatically).

- [ ] **Step 3: Run `go vet`**

```bash
cd infra && go vet ./...
```

Expected: no output (clean).

- [ ] **Step 4: Commit**

```bash
git add infra/cmd/main.go
git commit -m "feat(infra): add Pulumi entry point with env dispatch refs #13"
```

---

### Task 6: Makefile Targets

**Files:**
- Modify: `makefile`

- [ ] **Step 1: Append infra targets to root `makefile`**

Add at the end of `makefile`:

```makefile

# IaC targets — requires Pulumi CLI and ENV=<local|k8s|aws>
.PHONY: infra-up infra-down infra-preview

infra-up:
	@cd infra && pulumi up --stack=$(ENV) --yes

infra-down:
	@cd infra && pulumi destroy --stack=$(ENV) --yes

infra-preview:
	@cd infra && pulumi preview --stack=$(ENV)
```

- [ ] **Step 2: Verify make can parse the file (no syntax errors)**

```bash
make --dry-run infra-preview ENV=local 2>&1 | head -5
```

Expected: output containing `cd infra && pulumi preview --stack=local` (no "missing separator" errors).

- [ ] **Step 3: Commit**

```bash
git add makefile
git commit -m "chore: add infra-up/down/preview Makefile targets refs #13"
```

---

### Task 7: `infra/README.md`

**Files:**
- Create: `infra/README.md`

- [ ] **Step 1: Create `infra/README.md`**

```markdown
# GossamerDB Infrastructure (Pulumi Go)

Provisions GossamerDB clusters on local (kind) or AWS (EKS) using the Pulumi Go SDK.

## Prerequisites

- Go 1.21+
- [Pulumi CLI](https://www.pulumi.com/docs/install/) (`brew install pulumi/tap/pulumi`)
- For `local`: Docker Desktop or equivalent (kind uses Docker)
- For `aws`: AWS credentials configured (`aws configure` or env vars)

## Stack Init (first time)

```bash
cd infra
pulumi login --local          # file-based state, no Pulumi Cloud required
pulumi stack init local       # creates Pulumi.local.yaml if not present
```

## Run Commands

| Make target | What it does |
|-------------|--------------|
| `make infra-preview ENV=local` | Show planned changes, no-op |
| `make infra-up ENV=local` | Apply changes (create/update resources) |
| `make infra-down ENV=local` | Destroy all resources in the stack |

Replace `local` with `k8s` or `aws` as needed.

## Adding a New Environment

1. Create `infra/Pulumi.<env>.yaml` with the required config keys (see `Pulumi.local.yaml` as template).
2. Add the `Env` constant in `pkg/config/config.go`.
3. Add a `case config.Env<Name>:` branch in `cmd/main.go`.
4. Implement the provisioner in a new file `infra/pkg/<env>/`.
```

- [ ] **Step 2: Commit**

```bash
git add infra/README.md
git commit -m "docs(infra): add README with prereqs, stack init, run commands refs #13"
```

---

### Task 8: Final Verification + Draft PR

- [ ] **Step 1: Full build + vet + test from infra/**

```bash
cd infra && go build ./... && go vet ./... && go test ./...
```

Expected:
```
ok  	gossamerdb/infra/pkg/config
```
No errors from build or vet.

- [ ] **Step 2: Verify Makefile targets parse correctly**

```bash
make --dry-run infra-up ENV=local
make --dry-run infra-down ENV=local
make --dry-run infra-preview ENV=local
```

Expected: each outputs the `cd infra && pulumi ...` command without errors.

- [ ] **Step 3: Open draft PR targeting `feat/12-iac-pulumi-go`**

```bash
git push -u origin feat/12-iac-pulumi-go/infra-1-scaffold
gh pr create \
  --title "feat(infra): INFRA-1 IaC scaffold + config model (Pulumi Go)" \
  --body "$(cat <<'EOF'
## Summary

- Bootstraps `infra/` as an isolated Go module (`gossamerdb/infra`) with Pulumi Go SDK
- Adds typed `Config` struct with `Env` enum (local|k8s|aws) and `Validate()`
- `infra/cmd/main.go` dispatches to env-specific provisioner stubs (filled by INFRA-2/3)
- Root `makefile` gains `infra-up/down/preview ENV=<stack>` targets

Fixes #13

## Test Plan

- [x] `go build ./...` from `infra/` — clean
- [x] `go vet ./...` from `infra/` — clean
- [x] `go test ./pkg/config/...` — `TestValidate_*` pass
- [x] `make --dry-run infra-preview ENV=local` — parses correctly

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)" \
  --base feat/12-iac-pulumi-go \
  --draft
```

Expected: PR URL printed.

---

## Acceptance Criteria Checklist (from issue #13)

- [ ] `infra/go.mod` exists as a separate Go module (`gossamerdb/infra`); `go build ./...` passes from `infra/`
- [ ] `Config` struct fields: `Env` (enum: `local|k8s|aws`), `ClusterName`, `K8sVersion`, `NodeCount`, `AWSRegion`, `Namespace`
- [ ] `Config` loaded from `Pulumi.<stack>.yaml` via `pulumi.Config` — no hardcoded values
- [ ] `infra/cmd/main.go` registers `pulumi.Run` entry point that dispatches on `Config.Env`
- [ ] Makefile targets: `infra-up ENV=local`, `infra-down ENV=local`, `infra-preview ENV=local`
- [ ] `infra/README.md` covers prereqs, stack init, run commands, adding a new env
- [ ] `go vet ./...` clean; no unused imports
