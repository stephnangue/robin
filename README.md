# Robin

**A workload-identity injector.** Robin sits next to a workload, sources that workload's native identity, and presents it as a bearer token on each outbound request to a credential broker — so the workload never holds a long-lived credential.

> **Status: design stage.** No code yet — the architecture of record lives in [docs/design.md](docs/design.md), and the implementation is sequenced as a series of PRs there. This README describes the intended design.

## What Robin is

Application frameworks (kagent, generic OpenAI-compatible clients) have nowhere clean to put a dynamic, rotating credential — their credential fields expect a static secret. Robin removes the workload from the credential business:

- The workload presents **identity** (a short-lived token saying *who it is*), not a credential it holds.
- A downstream **broker** (Warden, or any identity-aware egress gateway) validates that identity and applies the real upstream credential.
- The machine holds **no real provider credential** — at most an inert placeholder.

Robin is deliberately thin: it **forwards native identity and does nothing else**. It never mints, exchanges, signs, or federates — any credential derivation happens at the broker. Because of that, the "no credential on the machine" property holds universally, for every provider.

```
 workload ──http, dummy/no cred──▶ Robin (127.0.0.1:4000 or UDS)
                                     │ resolve native identity via provider
                                     │ inject: Authorization: Bearer <tok>  (overwrites placeholder)
                                     ▼
                                  Broker ──validate identity──▶ apply real cred ──▶ upstream
```

It is a streaming reverse proxy with a pluggable identity provider. Every provider yields a bearer token, so Robin is body-agnostic and never reads or buffers the request body (SSE token streaming passes straight through).

**Threat model in one line:** anything on the pod's loopback can ask Robin to present the workload's identity — so Robin defaults to loopback-only, with a Unix-domain-socket + `SO_PEERCRED` peer-credential mode for hardened deployments (v0.2).

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
ROBIN_LISTEN_ADDR=127.0.0.1:4000 \
ROBIN_ADMIN_ADDR=:4001 \
  bin/robin
# app egress: point it at http://127.0.0.1:4000 ; probe http://<pod-ip>:4001/healthz
```

**Container image:**

```sh
docker build -t robin:0.1.0 -f deploy/Dockerfile .   # ~24MB distroless, nonroot
```

**Kubernetes native sidecar:** see [`deploy/k8s/sidecar-example.yaml`](deploy/k8s/sidecar-example.yaml) — Robin runs as an `initContainer` with `restartPolicy: Always` (K8s 1.29+), the app points its egress at `127.0.0.1:4000`, and probes hit the admin plane on `:4001`.

> Bind the **proxy** plane to `127.0.0.1` (only this pod's app should reach it); bind the **admin** plane to all interfaces (`:4001`) so the kubelet's liveness/readiness probes can reach it.

## Identity providers

| Source | `ROBIN_TOKEN_SOURCE` | Rotation |
|--------|----------------------|----------|
| Kubernetes projected ServiceAccount token | `file` | kubelet rotates the file in place (~80% TTL); Robin **re-reads per request**, never caches. |
| SPIFFE JWT-SVID (Workload API) | `jwtsvid` | Not pushed (unary fetch); Robin caches and **refreshes ahead of `exp`**, and serves a still-valid cached token if the agent briefly fails. |

## Configuration

Config is a flat set of `ROBIN_`-prefixed scalars — **no config language**. Primary plane is **environment variables** (idiomatic for sidecars/systemd); an optional flat `.env`-style `KEY=value` file is supported for standalone hosts. Precedence: **flags > environment > file**.

| Var | Default | Notes |
|-----|---------|-------|
| `ROBIN_UPSTREAM_URL` | (required) | Broker base URL |
| `ROBIN_TOKEN_SOURCE` | `file` | `file` \| `jwtsvid` |
| `ROBIN_LISTEN_ADDR` | `127.0.0.1:4000` | proxy plane (loopback by default) |
| `ROBIN_LISTEN_UDS` | — | UDS path; enables peer-cred mode (v0.2) |
| `ROBIN_ADMIN_ADDR` | `:4001` | health/readiness/metrics plane |
| `ROBIN_TOKEN_FILE` | `/var/run/secrets/tokens/token` | `file` provider |
| `ROBIN_AUDIENCE` | — | required for `jwtsvid`; **must match the broker** |
| `ROBIN_SPIFFE_SOCKET` | — | `jwtsvid` socket addr (optional; falls back to the go-spiffe default) |
| `ROBIN_SVID_REFRESH_BEFORE` | `60s` | refresh ahead of `exp` (clamped ≤ ½ the observed lifetime) |
| `ROBIN_UPSTREAM_CA_FILE` | — | verify broker TLS |
| `ROBIN_PEERCRED_ALLOW_UIDS` | — | (v0.2) comma-separated UIDs; empty = allow any local peer |

> **Bind address:** the proxy plane defaults to `127.0.0.1:4000` (loopback); the admin plane defaults to `:4001` (all interfaces) so kubelet probes can reach it — restrict `:4001` ingress with a NetworkPolicy where the platform allows it.

> **Audience must match end to end.** A mismatch between the token's audience and the broker's expected audience is a hard reject — for projected tokens and SVIDs alike.

## Deployment topologies

Same binary; the topology determines how identity is *sourced*, not what Robin does with it.

- **Native sidecar (default, supported).** An init container with `restartPolicy: Always` (Kubernetes 1.29+) so Robin starts before app containers (no first-call race) and terminates after them (no in-flight-egress loss). The workload reaches Robin on `localhost`.
- **Standalone systemd unit (v0.2).** Robin runs as a host/VM service; identity comes from a node-level SPIRE agent (`jwtsvid`).
- **Per-node DaemonSet** — *advanced/optional.* Fewer instances, but loses per-pod identity fidelity unless SPIRE does per-pod attestation.
- **Standalone egress service** — *generally an anti-pattern.* Loses transparent localhost injection and per-pod identity.

## Admin endpoints

Served on a **separate admin listener** (`ROBIN_ADMIN_ADDR`, default `:4001`) — *not* the proxy port, because the proxy forwards every path to the broker (a probe there would be proxied upstream and could leak identity).

- `GET /healthz` — liveness (always 200). *(v0.1)*
- `GET /readyz` — readiness; 200 only when an identity can be resolved. *(v0.2)*
- `GET /metrics` — Prometheus exposition (token fetch latency, cache hit/refresh, served-stale, upstream status codes). *(v0.2)*

## Failure semantics

| Condition | Response |
|-----------|----------|
| Identity unavailable (token file missing, Workload API down) | **503** — never forwards a missing/placeholder credential (jwtsvid serves a still-valid cached token first) |
| Broker unreachable | **502** — single attempt, no blind retry |
| Audience mismatch | fail closed with an explicit log (config error, not transient) |

## Security notes

- The token value is **never logged** — redaction is structural (it never enters a log record).
- The container runs **nonroot** on a static distroless base.
- Bearer-only by design (forwards a JWT-SVID / projected token). mTLS with an X.509-SVID (proof-of-possession) was considered and deliberately deferred; see [docs/design.md](docs/design.md), decision #13.
- Hardened local trust boundary via UDS + `SO_PEERCRED` UID allowlist (v0.2).

## When *not* to use Robin

- **You control the client's auth path.** An in-process `RoundTripper` / auth hook that fetches the identity is lighter than a proxy — no extra process, no localhost trust boundary. Robin exists for clients you *can't* modify (a static-string credential field).
- **You already run a service mesh** (Istio ambient / ztunnel / Cilium). Use its egress identity origination instead of adding a per-pod sidecar.

## Roadmap

- **v0.1 — native bearer core:** provider interface, `file` + `jwtsvid` providers, native-sidecar deployment, TCP loopback, broker-forward path, `/healthz`, structured logging.
- **v0.2 — hardening:** UDS + `SO_PEERCRED`, `/readyz`, Prometheus metrics, standalone systemd deployment.
- **Future:** per-request role/intent assertion travelling with the identity.

See [docs/design.md](docs/design.md) for the full architecture of record and the PR-by-PR implementation plan.

## License

[MPL-2.0](LICENSE).
