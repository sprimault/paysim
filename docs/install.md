> [🇬🇧 English](install.md) · [🇫🇷 Français](install.fr.md)

# Installation

Paysim is packaged like MailHog or OnlyOffice: a single container you
add to your stack, configure via environment variables, and consume
from your other services. This document covers Docker Compose,
Kubernetes (NodePort or Ingress), and the configuration knobs.

## Prerequisites

- **Docker** 20+ for local builds and Compose.
- **kubectl** 1.22+ if you deploy on Kubernetes (verb-based route
  matching requires Go 1.22 stdlib, exposed since kubectl 1.22).
- A cluster (k3s, k3d, kind, or anything else) for the K8s section.

## Configuration

All configuration goes through environment variables prefixed with
`PAYSIM_`. Any variable holding a secret also accepts a `_FILE`
suffix that reads the value from a file — useful for K8s Secret
mounts.

| Variable | Purpose |
|---|---|
| `PAYSIM_PUBLIC_URL` | URL the browser sees (ingress host, or `http://<node-ip>:30880` for NodePort). |
| `PAYSIM_CALLBACK_URL` | Default merchant callback URL for webhooks — used as fallback when a payment has no `returnUrl`. |
| `PAYSIM_BASE_PATH` | Prefix when Paysim is served under a sub-path (e.g. `/paysim`). Empty when served at root. |
| `PAYSIM_API_TOKEN` (+ `_FILE`) | Bearer token that protects the control API. Empty = open (local mode only). |
| `PAYSIM_PAYZEN_HMAC_KEY` (+ `_FILE`) | HMAC-SHA-256 key used to sign PayZen `kr-answer` payloads. |
| `PAYSIM_MAX_PAYMENTS` | Retention cap for in-memory storage. Default 10000. |
| `PAYSIM_LOG_LEVEL` | Log level (`debug`, `info`, `warn`, `error`). Default `info`. |
| `PAYSIM_STORE` | Storage backend: `memory` (default, stateless) or `sqlite`. |
| `PAYSIM_SQLITE_PATH` | Path to the SQLite file when `PAYSIM_STORE=sqlite`. Default `/data/paysim.db`. Requires a writable volume. |
| `PAYSIM_CHAOS_LATENCY_MS` | Injected latency on every REST V4 request. `0` disables. |
| `PAYSIM_CHAOS_ERROR_RATE` | Percentage of REST V4 requests returning a 500. `0-100`. |

## The two-URL matrix

Paysim manipulates two independent URLs:

- **`PAYSIM_PUBLIC_URL`** — the URL a **browser** uses to reach Paysim
  (redirects, absolute links rendered in the UI).
- **`PAYSIM_CALLBACK_URL`** — the default URL **Paysim** uses to deliver
  webhooks when a payment has no explicit `notificationUrl`. This is
  the **internal-network** view of the merchant, from the Paysim pod.

**They do not derive from each other** (invariant 7). Guessing one
from the other works locally but breaks in every Compose stack and
every cluster. The incoming request's `Host` header does not help
either: behind an ingress it lies.

| Scenario | `PAYSIM_PUBLIC_URL` | `PAYSIM_CALLBACK_URL` |
|---|---|---|
| Standalone local binary (dev) | `http://localhost:8080` | `http://localhost:<merchant-port>` |
| Merchant on host, Paysim in a container | `http://localhost:8080` | `http://host.docker.internal:<merchant-port>` |
| Merchant + Paysim in the same Compose | `http://localhost:8080` | `http://<merchant-service>:<internal-port>` (service DNS) |
| Kubernetes NodePort | `http://<node-ip>:30880` | `http://<merchant-svc>.<ns>.svc.cluster.local:<port>` |
| Kubernetes behind Ingress | `https://paysim.example.com` | `http://<merchant-svc>.<ns>.svc.cluster.local:<port>` |

**Edge case**: a payment can carry its own `notificationUrl` in the
`simulate` body — it takes precedence over `PAYSIM_CALLBACK_URL`.
Recommended for CI tests that deliver the webhook to a local mock
without depending on a shared environment variable.

## Option 1 — Docker Compose

The simplest path for local testing. Copy [`deploy/compose.yml`](../deploy/compose.yml) into your existing stack and set the environment variables.

```bash
docker compose -f deploy/compose.yml up -d
```

Browse to `http://localhost:8080/`.

To validate the full flow with a PHP merchant that receives and verifies webhooks:

```bash
docker compose -f deploy/compose.yml -f deploy/compose.demo.yml up
```

The demo merchant listens on port 9000 and writes verified signatures to
`examples/php/retours.log`.

## Option 2 — Kubernetes as an internal dev tool (NodePort)

This mirrors how MailHog is typically deployed: a NodePort service
accessible from any host on the cluster's network, at
`http://<node-ip>:30880`. No DNS, no TLS, no ingress required.

### 1. Build the image and load it into your cluster

For a local **k3d** cluster (Docker-in-Docker), you can import the
image directly without any registry:

```bash
# Build the OCI image
docker build -f deploy/Dockerfile -t paysim:local .

# Import it into your k3d cluster (replace 'mycluster' with your cluster name)
k3d image import paysim:local -c mycluster
```

For **k3s** or **kind**, adapt the import command:

```bash
# k3s: use ctr on each node
docker save paysim:local | sudo k3s ctr images import -

# kind
kind load docker-image paysim:local --name mycluster
```

To pull from a public/private registry instead (see the “From a registry” section below).

### 2. Update the image reference

Edit [`deploy/k8s/base/kustomization.yaml`](../deploy/k8s/base/kustomization.yaml) to point at the locally-imported image:

```yaml
images:
  - name: ghcr.io/sprimault/paysim
    newName: paysim
    newTag: local
```

### 3. Fill in the Secret

Edit [`deploy/k8s/base/secret.yaml`](../deploy/k8s/base/secret.yaml) with your HMAC key:

```yaml
stringData:
  PAYSIM_PAYZEN_HMAC_KEY: "your-hmac-secret-here"
  PAYSIM_API_TOKEN: ""  # empty = open
```

For production, use sealed-secrets, SOPS or external-secrets rather than committing the plain file.

### 4. Apply

```bash
kubectl apply -k deploy/k8s/
kubectl -n paysim rollout status deployment/paysim
```

Access the UI at `http://<any-node-ip>:30880/`.

## Option 3 — Kubernetes behind an ingress (domain + TLS + auth)

To expose Paysim on a public hostname with TLS and optional access
control, enable the ingress:

### 1. Uncomment the ingress line in kustomization

Edit [`deploy/k8s/base/kustomization.yaml`](../deploy/k8s/base/kustomization.yaml):

```yaml
resources:
  - namespace.yaml
  - configmap.yaml
  - secret.yaml
  - deployment.yaml
  - service.yaml
  - ingress.yaml    # enabled
```

### 2. Edit `deploy/k8s/base/ingress.yaml`

Set the hostname and uncomment the annotations you need:

**TLS via cert-manager** (assumes a working cluster-issuer):

```yaml
metadata:
  annotations:
    cert-manager.io/cluster-issuer: "letsencrypt-prod"
spec:
  tls:
    - hosts:
        - paysim.example.com
      secretName: paysim-tls
```

**Basic auth (nginx-ingress)** — protect the UI with a login prompt:

```bash
# Generate an htpasswd file
htpasswd -cB auth admin
# Create a Secret from it
kubectl -n paysim create secret generic paysim-basic-auth --from-file=auth
```

```yaml
metadata:
  annotations:
    nginx.ingress.kubernetes.io/auth-type: basic
    nginx.ingress.kubernetes.io/auth-secret: paysim-basic-auth
    nginx.ingress.kubernetes.io/auth-realm: "Paysim - authentication required"
```

**IP allow-list**:

```yaml
metadata:
  annotations:
    nginx.ingress.kubernetes.io/whitelist-source-range: "10.0.0.0/8,192.168.0.0/16"
```

### 3. Apply

```bash
kubectl apply -k deploy/k8s/
```

## From a registry (optional)

Instead of a local build + import, you can push the image to a
registry and pull it from the cluster. The default `kustomization.yaml`
references `ghcr.io/sprimault/paysim:latest` — swap in your own path.

**Build and push (multi-arch)**:

```bash
make image-push  # requires docker login and buildx configured
```

**Private registry**: create an `imagePullSecret` in the namespace
and reference it from the Deployment.

Public GHCR images pushed by CI will land here at the phase-6 public
release — for now, local build + import is the recommended path.

## SQLite persistence (optional)

By default Paysim keeps everything in memory — the read-only root
filesystem in the Deployment reflects this. To persist payments,
webhook history and bus events across restarts, apply the SQLite
overlay:

```bash
kubectl apply -k deploy/k8s/overlays/sqlite/
kubectl -n paysim rollout status deployment/paysim
```

The overlay adds:

- a `PersistentVolumeClaim` named `paysim-data` (1Gi, RWO) mounted at
  `/data`. Adjust the size in
  [`deploy/k8s/overlays/sqlite/pvc.yaml`](../deploy/k8s/overlays/sqlite/pvc.yaml)
  before applying if needed.
- `PAYSIM_STORE=sqlite` injected as an env var (defaults to
  `/data/paysim.db` — no need to set `PAYSIM_SQLITE_PATH` explicitly).

`readOnlyRootFilesystem` stays `true`: only `/data` is writable, via
the mounted volume. The rest of the container filesystem remains
immutable — same security posture as the memory-mode default.

The single-replica invariant is preserved (an RWO PVC can only be
bound to one pod at a time, and Paysim's in-memory state would not
be consistent across replicas anyway).

## Troubleshooting

- **Pod stays in `ImagePullBackOff`** — the image is not in the
  cluster's local store. For k3d, use `k3d image import`; for
  kind, `kind load docker-image`; for k3s, `k3s ctr images import`.
- **NodePort unreachable** — check your firewall (port 30880 by
  default) and that the node's IP is routable from where you're
  browsing.
- **`ImagePullBackOff` on a private registry** — you're missing an
  `imagePullSecret` in the namespace, or it doesn't match the image
  reference.

## Seeding demo data

Once Paysim is up, populate it with a varied dataset to see the UI
states (captured / declined / active / revoked / expired) without
crafting curl calls by hand. Useful for a first walkthrough:

Default `PAYSIM_URL` is `http://localhost:30880` (Kubernetes
NodePort). For a Docker Compose deployment exposed on port 8080,
override it accordingly.

```bash
# Bash / Linux / macOS / Windows git-bash
bash examples/seed-paysim.sh
# Docker Compose: PAYSIM_URL=http://localhost:8080 bash examples/seed-paysim.sh
```

```powershell
# Native Windows PowerShell
./examples/seed-paysim.ps1
# Docker Compose: $env:PAYSIM_URL = 'http://localhost:8080'; ./examples/seed-paysim.ps1
```

Both scripts create the same 11 cases: nominal capture, magic-amount
decline, authorization-only, Visa enrollment + monthly subscription
with 2 successful renewals, magic-PAN enrollment + failing renewal,
Mastercard / Amex / expired-card enrollments, and a manual revocation.

Options: `--purge` (bash) / `-Purge` (PowerShell) clears existing
payments first. Set `NOTIF_URL` if you want webhooks delivered
somewhere reachable (default: `http://localhost:1/discard` — a closed
port that fails immediately, keeping the queue view uncluttered).

## Merchant integration

The [`examples/php`](../examples/php/README.md) folder contains a full
PHP merchant that creates a payment, receives the signed webhook, and
verifies the `kr-hash` signature — the canonical reference for any
integration.
