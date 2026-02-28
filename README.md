# OtterScale Operator Template

[![Tests](https://github.com/otterscale/operator-template/actions/workflows/test.yml/badge.svg)](https://github.com/otterscale/operator-template/actions/workflows/test.yml)
[![Lint](https://github.com/otterscale/operator-template/actions/workflows/lint.yml/badge.svg)](https://github.com/otterscale/operator-template/actions/workflows/lint.yml)
[![codecov](https://codecov.io/gh/otterscale/operator-template/branch/main/graph/badge.svg)](https://codecov.io/gh/otterscale/operator-template)
[![Go Report Card](https://goreportcard.com/badge/github.com/otterscale/operator-template)](https://goreportcard.com/report/github.com/otterscale/operator-template)
[![Release](https://img.shields.io/github/v/release/otterscale/operator-template)](https://github.com/otterscale/operator-template/releases/latest)
[![License](https://img.shields.io/github/license/otterscale/operator-template)](LICENSE)

A GitHub repository template for building Kubernetes operators that reconcile [OtterScale](https://github.com/otterscale/api) custom resources. Scaffolded with [Kubebuilder](https://book.kubebuilder.io) v4.

## Quick Start

Click **"Use this template"** on GitHub to create a new repository, then clone it locally.

### Add an API Controller

Use `kubebuilder create api` with the external API flags to scaffold a controller for an OtterScale resource:

```bash
kubebuilder create api \
  --group addons --version v1alpha1 --kind Module \
  --controller=true --resource=false \
  --external-api-path=github.com/otterscale/api/addons/v1alpha1 \
  --external-api-domain=otterscale.io \
  --external-api-module=github.com/otterscale/api
```

| Flag                    | Purpose                                                      |
| ----------------------- | ------------------------------------------------------------ |
| `--controller=true`     | Generate a controller for reconciliation logic               |
| `--resource=false`      | Skip CRD generation (the CRD is defined in the external API) |
| `--external-api-path`   | Go import path of the external API types                     |
| `--external-api-domain` | API group domain (produces `addons.otterscale.io`)           |
| `--external-api-module` | Go module that provides the types                            |

This scaffolds:

- `internal/controller/module_controller.go` — reconciliation logic
- `internal/controller/module_controller_test.go` — test skeleton
- Registration in `cmd/main.go`

Repeat for additional resources by changing `--group`, `--version`, and `--kind`.

### After Scaffolding

```bash
go mod tidy
make manifests generate
```

### Add Aggregate RBAC Roles

Kubebuilder does not scaffold aggregate `ClusterRole` files for custom resources. You need to create them manually so that the default `admin`, `edit`, and `view` ClusterRoles automatically inherit permissions for your resource.

For each Kind (e.g. `Module`), create the following files under `config/rbac/`:

| File                      | Purpose                                               |
| ------------------------- | ----------------------------------------------------- |
| `module_admin_role.yaml`  | Full CRUD — aggregated into the `admin` ClusterRole   |
| `module_editor_role.yaml` | Read + write — aggregated into the `edit` ClusterRole |
| `module_viewer_role.yaml` | Read-only — aggregated into the `view` ClusterRole    |

Each file is a `ClusterRole` with the appropriate `rbac.authorization.k8s.io/aggregate-to-*` label set to `"true"`.

> **Note:** The `aggregate-to-admin`, `aggregate-to-edit`, and `aggregate-to-view` labels only apply to **namespaced** resources, because the built-in `admin` / `edit` / `view` ClusterRoles are designed for namespace-scoped access (bound via `RoleBinding`). If your resource is **cluster-scoped**, these aggregate labels have no effect; use `aggregate-to-cluster-admin` instead, or create dedicated `ClusterRoleBinding`s.

Example for a `Module` resource in the `addons.otterscale.io` API group:

**`config/rbac/module_admin_role.yaml`**

```yaml
# This rule is not used by the project operator-template itself.
# It is provided to allow the cluster admin to help manage permissions for users.
#
# Grants read-only access to addons.otterscale.io resources.
# This role is intended for users who need visibility into these resources
# without permissions to modify them. It is ideal for monitoring purposes and limited-access viewing.

apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  labels:
    app.kubernetes.io/name: operator-template
    app.kubernetes.io/managed-by: kustomize
    rbac.authorization.k8s.io/aggregate-to-admin: "true"
  name: module-admin-role
rules:
  - apiGroups:
      - addons.otterscale.io
    resources:
      - modules
    verbs:
      - "*"
  - apiGroups:
      - addons.otterscale.io
    resources:
      - modules/status
    verbs:
      - get
```

**`config/rbac/module_editor_role.yaml`**

```yaml
# This rule is not used by the project operator-template itself.
# It is provided to allow the cluster admin to help manage permissions for users.
#
# Grants read-only access to addons.otterscale.io resources.
# This role is intended for users who need visibility into these resources
# without permissions to modify them. It is ideal for monitoring purposes and limited-access viewing.

apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  labels:
    app.kubernetes.io/name: operator-template
    app.kubernetes.io/managed-by: kustomize
    rbac.authorization.k8s.io/aggregate-to-edit: "true"
  name: module-editor-role
rules:
  - apiGroups:
      - addons.otterscale.io
    resources:
      - modules
    verbs:
      - create
      - delete
      - get
      - list
      - patch
      - update
      - watch
  - apiGroups:
      - addons.otterscale.io
    resources:
      - modules/status
    verbs:
      - get
```

**`config/rbac/module_viewer_role.yaml`**

```yaml
# This rule is not used by the project operator-template itself.
# It is provided to allow the cluster admin to help manage permissions for users.
#
# Grants read-only access to addons.otterscale.io resources.
# This role is intended for users who need visibility into these resources
# without permissions to modify them. It is ideal for monitoring purposes and limited-access viewing.

apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  labels:
    app.kubernetes.io/name: operator-template
    app.kubernetes.io/managed-by: kustomize
    rbac.authorization.k8s.io/aggregate-to-view: "true"
  name: module-viewer-role
rules:
  - apiGroups:
      - addons.otterscale.io
    resources:
      - modules
    verbs:
      - get
      - list
      - watch
  - apiGroups:
      - addons.otterscale.io
    resources:
      - modules/status
    verbs:
      - get
```

Then register them in `config/rbac/kustomization.yaml`:

```yaml
resources:
  # For each CRD, "Admin", "Editor" and "Viewer" roles are scaffolded by
  # default, aiding admins in cluster management. Those roles are
  # not used by the operator-template itself. You can comment the following lines
  # if you do not want those helpers be installed with your Project.
  - module_admin_role.yaml
  - module_editor_role.yaml
  - module_viewer_role.yaml
```

## Versioning

The `cmd/main.go` file declares a `version` variable (defaults to `"devel"`). This value is injected at build time via `-ldflags`:

```go
var version = "devel"
```

Both the `Makefile` and `Dockerfile` automatically pass `VERSION` (derived from `git describe --tags --always`) through `-ldflags "-X main.version=$(VERSION)"`. If you add version-dependent logic (e.g. logging, health endpoints, or user-agent strings), make sure to reference `version` from `cmd/main.go`.

> **Note:** When customizing the build process or adding a new build target, remember to include `-ldflags "-X main.version=$(VERSION)"` so the binary embeds the correct version.

## Development

### Prerequisites

- Go 1.26+
- Docker 17.03+
- kubectl v1.11.3+
- Access to a Kubernetes cluster

### Run Locally

```bash
make run
```

### Run Tests

```bash
make test
```

### Lint

```bash
make lint      # check
make lint-fix  # auto-fix
```

## Deployment

### Build & Push Image

```bash
export IMG=<registry>/<project>:tag
make docker-build docker-push IMG=$IMG
```

### Deploy to Cluster

```bash
make deploy IMG=$IMG
```

### Undeploy

```bash
make undeploy
```

## CI / CD

This template includes GitHub Actions workflows out of the box:

| Workflow        | Trigger               | Description                                                |
| --------------- | --------------------- | ---------------------------------------------------------- |
| **Lint**        | push, PR              | Runs `golangci-lint`                                       |
| **Tests**       | push, PR              | Runs `make test` (unit tests via envtest)                  |
| **E2E Tests**   | push, PR              | Runs end-to-end tests on a Kind cluster                    |
| **Publish**     | release published     | Builds & pushes image to `ghcr.io`, uploads `install.yaml` |
| **Auto Update** | weekly (Tue) / manual | Checks for Kubebuilder scaffold updates                    |

## Distribution

### YAML Bundle

```bash
make build-installer IMG=<registry>/<project>:tag
```

Users install with:

```bash
kubectl apply -f https://raw.githubusercontent.com/<org>/<repo>/<tag>/dist/install.yaml
```

### Helm Chart

```bash
kubebuilder edit --plugins=helm/v2-alpha
```

A chart will be generated under `dist/chart/`.

## License

Copyright 2026 The OtterScale Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
