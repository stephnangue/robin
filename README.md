# Robin

**A workload-identity injector.** Robin runs beside a workload, sources its short-lived, rotating identity, and injects it as a fresh bearer token on every outbound request — so the application never has to know the credential exists, let alone that it rotates.

## The problem

Kubernetes increasingly hands workloads **short-lived, rotating credentials**. Projected ServiceAccount tokens roll over roughly hourly; SPIFFE JWT-SVIDs every few minutes. That is exactly what you want for security — a leaked token expires on its own, fast.

The catch: **almost no application knows how to consume one.** An app reads its API key or bearer token *once* — from an environment variable, a config field, an `Authorization` header set when its HTTP client is constructed — and then holds that value for its entire lifetime. There is no hook to reload it, no callback when it rotates. Point such an app at a rotating token and it keeps presenting the stale one; minutes (or an hour) later, every request starts failing with `401`.

So teams fall back to the very thing rotation was meant to eliminate: a **long-lived static secret**, mounted into the app and held there indefinitely. Now the application *is* part of the credential plane — it holds a real secret, and rotating that secret means redeploying.

Robin breaks the coupling. It sits next to the workload, sources the rotating identity from the platform, and injects a **fresh** bearer on every request. The application points its egress at Robin and carries **no credential at all** — not even a rotating one. It presents *identity*; Robin keeps it current; a downstream broker turns that identity into the real upstream credential.

## How it works

```
 app ──http, no credential──▶ Robin (127.0.0.1:4000)
                                │ resolve the workload's rotating identity (always fresh)
                                │ inject: Authorization: Bearer <token>   (overwrites any placeholder)
                                ▼
                             broker ──validate identity──▶ apply real credential ──▶ upstream
```

- The **app** makes ordinary HTTP requests to Robin on loopback. No API key, no token-reload logic — at most an inert placeholder header, which Robin overwrites.
- **Robin** is a streaming reverse proxy with a pluggable identity provider. It resolves the workload's native token (always current), sets `Authorization: Bearer`, and forwards. It is body-agnostic and never reads or buffers the request body, so streaming responses pass straight through.
- The **broker** — any identity-aware egress gateway — validates the presented identity and applies the real upstream credential. Robin itself never mints, exchanges, signs, or federates, so the machine holds **no real provider credential** — universally, for every provider, with no exceptions.

**Threat model in one line:** anything on the pod's loopback can ask Robin to present the workload's identity — so the proxy plane defaults to loopback-only, with a Unix-domain-socket + `SO_PEERCRED` peer-credential mode for hardened deployments.

## Quickstart

**Build:**

```sh
make build            # -> bin/robin (static, CGO-free)
make test             # go test ./...
```

**Run locally** (file provider, pointing at any broker/echo endpoint):

```sh
ROBIN_UPSTREAM_URL=https://broker.example:8443 \
ROBIN_TOKEN_SOURCE=file \
ROBIN_TOKEN_FILE=/var/run/secrets/tokens/broker-token \
  bin/robin
# app egress -> http://127.0.0.1:4000 ; probe http://<pod-ip>:4001/healthz
```

**Container image:**

```sh
docker build -t robin:0.1.0 -f deploy/Dockerfile .   # ~14MB distroless, nonroot
```

**Kubernetes native sidecar:** see [`deploy/k8s/sidecar-example.yaml`](deploy/k8s/sidecar-example.yaml) — Robin runs as an `initContainer` with `restartPolicy: Always` (K8s 1.29+), the app points its egress at `127.0.0.1:4000`, and probes hit the admin plane on `:4001`.

## Identity providers

| Source | `ROBIN_TOKEN_SOURCE` | Rotation handling |
|--------|----------------------|-------------------|
| Kubernetes projected ServiceAccount token | `file` | the kubelet rotates the file in place (~80% TTL); Robin **re-reads per request**, so it never serves a stale token. |
| SPIFFE JWT-SVID (Workload API) | `jwtsvid` | not pushed (unary fetch), so Robin caches and **refreshes ahead of `exp`**, and serves a still-valid cached token if the agent briefly fails. |

Any other source that yields the workload's own short-lived OIDC/JWT identity fits the same shape: fetch it, forward it, let the broker validate.

## Configuration

Config is a flat set of `ROBIN_`-prefixed scalars — **no config language**. The primary plane is **environment variables** (idiomatic for sidecars and systemd units); an optional flat `.env`-style `KEY=value` file is supported for standalone hosts. Precedence: **flags > environment > file**.

| Var | Default | Notes |
|-----|---------|-------|
| `ROBIN_UPSTREAM_URL` | (required) | broker base URL |
| `ROBIN_TOKEN_SOURCE` | `file` | `file` \| `jwtsvid` |
| `ROBIN_LISTEN_ADDR` | `127.0.0.1:4000` | proxy plane (loopback by default) |
| `ROBIN_LISTEN_UDS` | — | UDS path; enables peer-cred mode |
| `ROBIN_ADMIN_ADDR` | `:4001` | health/readiness/metrics plane |
| `ROBIN_TOKEN_FILE` | `/var/run/secrets/tokens/token` | `file` provider |
| `ROBIN_AUDIENCE` | — | required for `jwtsvid`; **must match the broker** |
| `ROBIN_SPIFFE_SOCKET` | — | `jwtsvid` socket addr (optional; falls back to the go-spiffe default) |
| `ROBIN_SVID_REFRESH_BEFORE` | `60s` | refresh ahead of `exp` (clamped ≤ ½ the observed lifetime) |
| `ROBIN_UPSTREAM_CA_FILE` | — | verify broker TLS |
| `ROBIN_PEERCRED_ALLOW_UIDS` | — | comma-separated UIDs; empty = allow any local peer |

> **Bind address:** the proxy plane defaults to `127.0.0.1:4000` (loopback); the admin plane defaults to `:4001` (all interfaces) so kubelet probes can reach it — restrict `:4001` ingress with a NetworkPolicy where the platform allows it.

> **Audience must match end to end.** A mismatch between the token's audience and the broker's expected audience is a hard reject — for projected tokens and SVIDs alike.

## Deployment topologies

Same binary; the topology determines how identity is *sourced*, not what Robin does with it.

- **Native sidecar (default).** An init container with `restartPolicy: Always` (Kubernetes 1.29+) so Robin starts before the app container (no first-call race) and stops after it (no in-flight-egress loss). The workload reaches Robin on `localhost`.
- **Standalone systemd unit.** Robin runs as a host/VM service; identity comes from a node-level SPIRE agent (`jwtsvid`).
- **Per-node DaemonSet** — *advanced/optional.* Fewer instances, but loses per-pod identity fidelity unless SPIRE does per-pod attestation.
- **Standalone egress service** — *generally an anti-pattern.* Loses transparent localhost injection and per-pod identity.

## Admin endpoints

Served on a **separate admin listener** (`ROBIN_ADMIN_ADDR`, default `:4001`) — *not* the proxy port, because the proxy forwards every path to the broker (a probe there would be proxied upstream and could leak identity).

- `GET /healthz` — liveness (always 200).
- `GET /readyz` — readiness; 200 only when an identity can actually be resolved.
- `GET /metrics` — Prometheus exposition (token fetch latency, cache hit/refresh, served-stale, upstream status codes). *(planned)*

## Failure semantics

| Condition | Response |
|-----------|----------|
| Identity unavailable (token file missing/empty, Workload API down) | **503** — never forwards a missing/placeholder credential (jwtsvid serves a still-valid cached token first) |
| Broker unreachable | **502** — single attempt, no blind retry |
| Audience mismatch | fail closed with an explicit log (config error, not transient) |

## Security notes

- The token value is **never logged** — redaction is structural; it never enters a log record.
- The container runs **nonroot** on a static distroless base.
- Bearer-only by design (forwards a JWT-SVID / projected token). mTLS with an X.509-SVID (proof-of-possession) was considered and deliberately deferred.
- The proxy plane binds loopback by default; a Unix-domain-socket + `SO_PEERCRED` UID allowlist hardens the local trust boundary further.

## When *not* to use Robin

- **You control the client's auth path.** An in-process `RoundTripper` / auth hook that fetches the identity is lighter than a proxy — no extra process, no localhost trust boundary. Robin exists for apps you *can't* teach to rotate.
- **You already run a service mesh** (Istio ambient / ztunnel / Cilium). Use its egress identity origination instead of adding a per-pod sidecar.

## Roadmap

- **v0.1 (core):** `file` + `jwtsvid` providers, native-sidecar deployment, loopback proxy with broker-forward, `/healthz` + `/readyz`, structured logging, container image.
- **v0.2 (hardening):** `SO_PEERCRED` enforcement on the UDS path, Prometheus `/metrics`, standalone systemd deployment.
- **Future:** per-request role/intent assertion travelling with the identity.

## License

[MPL-2.0](LICENSE).
