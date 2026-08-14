> [🇬🇧 English](install.md) · [🇫🇷 Français](install.fr.md)

# Installation

Paysim ships as a single container you add to your stack, configure
via environment variables, and consume from your other services.
This document covers Docker Compose, Kubernetes (NodePort or Ingress),
and the configuration knobs.

> **Shell examples**: `bash` snippets below assume Git Bash on Windows
> or a POSIX shell. For native Windows PowerShell, replace
> `VAR=value cmd` with `$env:VAR="value"; cmd`, and `curl` with
> `Invoke-RestMethod`. Docker CLI commands are identical on both.

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
| `PAYSIM_PUBLIC_URL` | URL the browser sees (ingress host, or `http://<node-ip>:30890` for NodePort). |
| `PAYSIM_CALLBACK_URL` | Default merchant callback URL for webhooks — used as fallback when a payment has no `returnUrl`. |
| `PAYSIM_BASE_PATH` | Prefix when Paysim is served under a sub-path (e.g. `/paysim`). Empty when served at root. |
| `PAYSIM_API_TOKEN` (+ `_FILE`) | Bearer token that protects the control API for **server-to-server calls** (CI, scripts, tests). **Disables the web UI** if set — the SPA has no login flow and does not inject a Bearer token in its fetch calls. To protect the UI in a shared environment, use ingress-level basic auth (see [Option 3 — Ingress](#option-3--kubernetes-behind-an-ingress-domain--tls--auth)). Empty = open. |
| `PAYSIM_PAYZEN_HMAC_KEY` (+ `_FILE`) | HMAC-SHA-256 key signing the **browser return** (`kr-hash-key: sha256_hmac`). |
| `PAYSIM_PAYZEN_REST_PASSWORD` (+ `_FILE`) | REST API password signing **server-to-server notifications** (`kr-hash-key: password`). **Required** as soon as the HMAC key is set — see below. |
| `PAYSIM_PAYZEN_BRAND` | Lyra brand for traffic arriving on the protocol routes: `payzen` (default), `systempay`, `sogecommerce`, `scellius`, `lyra`. Those routes carry no brand — at Lyra the host designates it, and Paysim has only one. The control API takes it from the JSON body instead, so one instance can host several integrations. An unknown value stops startup. |
| `PAYSIM_MAX_PAYMENTS` | Retention cap for `PAYSIM_STORE=memory`. Default 10000. Beyond it, the oldest payments by creation date stop being retained — creation never fails, they simply become unreadable. No effect in SQLite mode, where retention is bounded by the disk. |
| `PAYSIM_LOG_LEVEL` | Log level (`debug`, `info`, `warn`, `error`). Default `info`. |
| `PAYSIM_STORE` | Storage backend: `memory` (default, stateless) or `sqlite`. |
| `PAYSIM_SQLITE_PATH` | Path to the SQLite file when `PAYSIM_STORE=sqlite`. Default `/data/paysim.db`. Requires a writable volume. |
| `PAYSIM_AUTOPLAY` | Play every payment as soon as it is created, without waiting for a simulation call. Default `false`. See below. |
| `PAYSIM_CHAOS_LATENCY_MS` | Injected latency on every REST V4 request. `0` disables. |
| `PAYSIM_CHAOS_ERROR_RATE` | Percentage of REST V4 requests returning a 500. `0-100`. |

## The two PayZen keys

PayZen does not sign both of its channels with the same key, and it
states which one it used in the `kr-hash-key` POST field:

| `kr-hash-key` | Channel | Paysim variable | Merchant-side key |
| --- | --- | --- | --- |
| `sha256_hmac` | Browser return | `PAYSIM_PAYZEN_HMAC_KEY` | shop HMAC key |
| `password` | Server notification (IPN) | `PAYSIM_PAYZEN_REST_PASSWORD` | REST API password |

The merchant SDK picks its key from that field — the official PHP SDK
does it explicitly:

```php
if ($_POST['kr-hash-key'] == "sha256_hmac") { $key = $this->_hashKey; }
elseif ($_POST['kr-hash-key'] == "password") { $key = $this->_password; }
```

**Both variables are therefore required**, and Paysim refuses to start
if the second one is missing. Signing everything with the browser key
would "work" here, but would leave the `password` branch of the merchant
code unexercised until production — where its IPN would fail although it
passed in testing. That is exactly the kind of fake worth not having.

Both values may be identical locally; what matters is that the merchant
takes the right verification path.

## `PAYSIM_AUTOPLAY` — end-to-end tests without a cardholder

In production, nothing happens after `CreatePayment` until the
cardholder authenticates on the form and the PSP notifies the outcome.
Paysim reproduces that faithfully: a fresh payment stays `initiated`
until something plays it, which is what a simulation call does.

An automated end-to-end test has nobody to play that part. The tempting
workaround — calling the simulation API from the merchant's own code —
is a bad trade: it pulls simulator mechanics into business code and
validates a sequence that will never exist in production, so it proves
nothing and becomes dead code the day you switch to the real PSP.

`PAYSIM_AUTOPLAY=true` plays the act server-side instead. No client
drives anything, the REST contract stays untouched, and a payment is
captured — or declined — the moment it is created, notification
included.

**The outcome still comes from the magic values.** A magic amount, a
decline PAN or an expired card decide exactly as they do otherwise: this
mode automates *who plays*, not *what comes out*. Every lever in
[testing-cards.md](testing-cards.md) keeps working unchanged.

**What it gives up**: `ABANDONED` and `EXPIRED` assume a cardholder who
never finishes. With autoplay on, no payment is ever left hanging, so
those outcomes require an explicit simulation call — and therefore this
mode off. That is why it is opt-in, and why it has no business being
enabled outside a test environment.

The server logs a `WARN` at startup when it is active.

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
| Merchant on host, Paysim in a container | `http://localhost:30880` | `http://host.docker.internal:<merchant-port>` |
| Merchant + Paysim in the same Compose | `http://localhost:30880` | `http://<merchant-service>:<internal-port>` (service DNS) |
| Kubernetes NodePort | `http://<node-ip>:30890` | `http://<merchant-svc>.<ns>.svc.cluster.local:<port>` |
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

Browse to `http://localhost:30880/`.

Override `PAYSIM_HOST_PORT` if `30880` is taken — `PAYSIM_PUBLIC_URL`
follows automatically:

```bash
PAYSIM_HOST_PORT=30890 docker compose -f deploy/compose.yml up -d
# now http://localhost:30890/
```

To validate the full flow with a PHP merchant that receives and verifies webhooks:

```bash
docker compose -f deploy/compose.yml -f deploy/compose.demo.yml up
```

The demo merchant listens on port 30881 (override with
`MERCHANT_HOST_PORT`) and writes verified signatures to
`examples/php/retours.log`.

## Option 2 — Kubernetes as an internal dev tool (NodePort)

A NodePort service accessible from any host on the cluster's
network, at `http://<node-ip>:30890`. No DNS, no TLS, no ingress
required. (Port `30880` is left free for the Docker Compose default,
so both can coexist on the same machine.)

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

Edit [`deploy/k8s/base/secret.yaml`](../deploy/k8s/base/secret.yaml) with both PayZen keys:

```yaml
stringData:
  PAYSIM_PAYZEN_HMAC_KEY: "your-hmac-secret-here"
  PAYSIM_PAYZEN_REST_PASSWORD: "your-rest-password-here"
  PAYSIM_API_TOKEN: ""  # empty = open
```

Both are required: the pod refuses to start if the second one is missing.

For production, use sealed-secrets, SOPS or external-secrets rather than committing the plain file.

### 4. Apply

```bash
kubectl apply -k deploy/k8s/
kubectl -n paysim rollout status deployment/paysim
```

Access the UI at `http://<any-node-ip>:30890/`.

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

Public GHCR images pushed by CI will land here at the public
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
- **NodePort unreachable** — check your firewall (port 30890 by
  default) and that the node's IP is routable from where you're
  browsing.
- **`ImagePullBackOff` on a private registry** — you're missing an
  `imagePullSecret` in the namespace, or it doesn't match the image
  reference.

## Seeding demo data

Once Paysim is up, populate it with a varied dataset to see the UI
states (captured / declined / active / revoked / expired) without
crafting curl calls by hand. Useful for a first walkthrough:

Default `PAYSIM_URL` is `http://localhost:30880` — works out of the
box for both Docker Compose and Kubernetes NodePort. Override
`PAYSIM_URL` only if you deployed Paysim on a different port or host.

```bash
# Bash / Linux / macOS / Windows git-bash
bash examples/seed-paysim.sh
```

```powershell
# Native Windows PowerShell
./examples/seed-paysim.ps1
```

Both scripts — and both `demo-ui` variants below — create the same
dataset, in two parts.

Notable cases first, each there to show one thing: an enrollment
carrying a full customer context, a nominal capture, three declines
with distinct reasons (51, 43, 91), a pending payment, an
authorization-only, an expired payment method, a revoked one, a
subscription with two successful renewals, one whose renewal is
declined, a cancelled one, one with no billing yet, and a one-click
replay.

Replays too, in different numbers on three payments — one on
`CMD-1042`, two on `CMD-1047`, three on `CMD-2012`. The badge on the
replay button counts them, and without them it would never show.

Volume next: thirty payments spread across the states. That part is
not decoration — search, state filters, pagination and the sticky
table header cannot be judged on eight rows. States are assigned by
rank rather than at random, so two runs give the same screen.

**Two declined instalments, two reasons to compare.** `SUB-78` fails
because its payment method was revoked *after* the subscription was
created: no bank code, it is not an issuer declining. `SUB-81` fails
for insufficient funds and carries a `51`.

The second case uses the overdrawn test PAN, which does enrol: a
verification commits no amount, so it does not query the balance. The
card only declines on the first debit — and that is the only lever
available on a schedule, whose amount is fixed and therefore cannot be
used as a magic amount.

Options: `--purge` (bash) / `-Purge` (PowerShell) clears existing
payments first. Set `NOTIF_URL` if you want webhooks delivered
somewhere reachable (default: `http://localhost:1/discard` — a closed
port that fails immediately, keeping the queue view uncluttered).

### Bringing the whole stack up in one command

`seed-paysim.sh` assumes Paysim is already running. To avoid starting
anything yourself, `examples/demo-ui.sh` brings up the whole set —
Paysim, a webhook receiver, and the Docker network linking them — then
seeds it:

```bash
# Bash / Linux / macOS / Windows git-bash
bash examples/demo-ui.sh
```

```powershell
# Native Windows PowerShell
./examples/demo-ui.ps1
```

**Docker must be installed and running** — on Windows or macOS that means
Docker Desktop started, not merely installed. Both scripts check this
upfront and stop with a clear message otherwise.

Beyond Docker, the bash version needs `curl` and `grep`; the
PowerShell one needs nothing extra, as `Invoke-RestMethod` is built in.
Both print the UI URL when done.

Change the port with `PORT` / `-Port`, the image with `IMAGE` / `-Image`
(`:edge` for the state of the main branch). On systems without
`hostname -I` — macOS in particular — set `HOST_IP` to the address your
browser uses to reach the machine; under PowerShell, `-HostIp` defaults
to `localhost`.

Tearing it down is a single command, echoed at the end of the run:

```bash
docker rm -f paysim-demo paysim-sink && docker network rm paysim-demo-net
```

Its dataset is chosen for what shows on screen: a full customer context,
three declines with distinct reasons, a payment left pending, an
unusable payment method, and a subscription with one billing run.

## Merchant integration

The [`examples/php`](../examples/php/README.md) folder contains a full
PHP merchant that creates a payment, receives the signed webhook, and
verifies the `kr-hash` signature — the canonical reference for any
integration.
