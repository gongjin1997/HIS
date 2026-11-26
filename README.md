# kinstall

Offline-first, agentless, cross-architecture (`amd64` / `arm64-4K` / `arm64-64K`) enterprise infrastructure & middleware installation and lifecycle platform.

## Architecture Highlights

- **Single Static Binary (`CGO_ENABLED=0`)**: Embedded SQLite (`modernc.org/sqlite`, WAL + 1-write/4-read pools) for single-node CLI & Embedded Vue 3.5 Web Console (`//go:embed web/dist/**`), seamlessly scaling to HA PostgreSQL 18 + direct `LISTEN/NOTIFY` multi-replica Server. Build lightweight CLI-only binaries with `-tags=noui`.
- **OS & Cross-Arch Governance (`spec/`, `internal/osadapter`)**: 42 OS match rules across 7 families (RHEL/Rocky/Alma/openEuler/Kylin V10 SP3 64K/Kylin V11 4K/UOS/Debian/Ubuntu/SLES), 71 golden fixtures, 2,880 `pkgmap` resolutions, and 1-RTT POSIX Facts collector.
- **Agentless SSH Engine (`internal/executor/sshx`, `internal/engine`)**: Dual-layer concurrency (`MaxSessions=8`, `MaxStartups=6`), ProxyJump ref-counted bastion sharing, `sudo-rs#1668` safe elevation, Async Detached `runs/<id>/`, 3-channel atomic upload, Fencing Token lease renewal, 3-Way Diff (`managed`/`create-only`/`managed-block`), and 15 built-in idempotent Tasks.
- **Supply Chain & Ternary Media Resolution (`internal/artifact`)**: Local / Huawei OBS·S3 (S3v4 `WhenRequired`) / Online priority waterfall with `singleflight` dedup, `offlineStrict` hard block (`KI-SRC-0005`), SigV4 15-min presigned URLs, pure-Go SM2/SM3/SM4 (`emmansun/gmsm`), Zot OCI mirror, and shared JDK/Maven/Node runtimes.
- **Enterprise Control Plane (`internal/server`, `internal/authz`, `internal/audit`, `internal/compliance`)**: Argon2id + 5-fail 423 lockout, 5-role RBAC, Four-Eyes Approval with DB-enforced `chk_no_self_approval`, fork-free dual SHA-256 + SM3 audit hash chain, lossless `Last-Event-ID` SSE, short-lived Kubeconfigs (`kinstall:cluster-admin` max 4h, never `system:masters`), DJCP Level-3 & GM/T-0054 evidence packaging.
- **12 Built-in Plugins (26 Products, `plugins/`)**: Strictly isolated to `pkg/sdk/v1alpha1` (enforced by `golangci-lint` `depguard`) — Kubernetes 1.35–1.38, Valkey 9.x / Redis 8.x, PostgreSQL 18.7/19.0 + Patroni/pgBackRest, Apache Kafka 4.x KRaft, OpenSearch 3.10 / ES, Fluent Bit + Data Prepper ELK Pipeline, MySQL 8.4/9.7 InnoDB Cluster, SeaweedFS 4.54 / Ceph RGW / RustFS / Garage, SkyWalking 11.1 + BanyanDB, DevOps Stack (Harbor/GitLab/Forgejo/Jenkins/SonarQube/Reposilite/Verdaccio/Nginx), Messaging/TX (RocketMQ 5.5/Seata 2.6/ZooKeeper 3.9), Data/AI/Apps (Milvus 2.6.26+/MongoDB/Neo4j/OnlyOffice/Rancher/Zabbix/`generic-app`), plus declarative YAML & external JSON-RPC plugin sandboxes.
- **Offline Vue 3.5 Web Console (`web/`)**: `DESIGN.md` v2.0 3-tier tokens, Dark/Light themes, 40px/32px density, zero external CDN, CSP-compliant `theme-boot.js`, JSON-Schema `x-kinstall-ui` dynamic forms, 3-Way Diff viewer, `@xterm/xterm` SSE terminal, Vue Flow 2.0 (`@xyflow/vue`) Stack DAG canvas, and Ternary Media Policy Center.

## Quick Start

```bash
# Build static binary with embedded Vue 3 Web Console
CGO_ENABLED=0 go build -o bin/kinstall ./cmd/kinstall

# Inspect built-in plugins and version
./bin/kinstall version
./bin/kinstall plugin list

# Start Control Plane + Embedded Web UI (default admin / Kinstall#2026!)
./bin/kinstall server start --listen :8080
```

## Development & Verification

```bash
# 1. Verify OS match rules & docs consistency
python3 spec/gen_os_docs.py --check

# 2. Lint (depguard R1-R6, gochecknoinits, gosec, errcheck, staticcheck)
golangci-lint run ./...

# 3. Full race-detector test suite
go test -race -count=1 ./...

# 4. Rebuild Web Console (TypeScript + Vite)
cd web && npm install && npm run build
```

## Documentation

Complete architecture, deep-design (`D1–D3`, `G1–G3`, `P1–P10`, `R1–R2`), visual system (`DESIGN.md`), and Web Console (`WEB_DESIGN.md`) specifications live under [`docs/`](./docs/).
