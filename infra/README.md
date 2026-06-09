# GossamerDB Infrastructure (Pulumi Go)

Provisions GossamerDB clusters on local (kind) or AWS (EKS) using the Pulumi Go SDK.

## Prerequisites

- Go 1.25+ (required by Pulumi SDK v3)
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
