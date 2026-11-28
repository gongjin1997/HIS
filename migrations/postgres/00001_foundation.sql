-- +goose Up
-- =============================================================================
-- Kinstall v2 Unified SSOT Foundation Schema (PostgreSQL 15+)
-- =============================================================================

CREATE TABLE IF NOT EXISTS workspaces (
    id          TEXT PRIMARY KEY,
    code        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
    id            TEXT PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    display_name  TEXT NOT NULL,
    email         TEXT NOT NULL DEFAULT '',
    role          TEXT NOT NULL DEFAULT 'viewer',
    password_hash TEXT NOT NULL,
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS api_tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    token_prefix TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    role         TEXT NOT NULL,
    expires_at   TEXT NOT NULL DEFAULT '',
    last_used_at TEXT NOT NULL DEFAULT '',
    revoked      INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS credentials (
    id             TEXT PRIMARY KEY,
    workspace_id   TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    kind           TEXT NOT NULL,
    username       TEXT NOT NULL DEFAULT '',
    secret_enc     TEXT NOT NULL,
    passphrase_enc TEXT NOT NULL DEFAULT '',
    description    TEXT NOT NULL DEFAULT '',
    created_by     TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    UNIQUE(workspace_id, name)
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    actor        TEXT NOT NULL,
    actor_role   TEXT NOT NULL DEFAULT '',
    action       TEXT NOT NULL,
    target_type  TEXT NOT NULL,
    target_id    TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'ok',
    detail       TEXT NOT NULL DEFAULT '',
    source_ip    TEXT NOT NULL DEFAULT '',
    prev_hash    TEXT NOT NULL,
    hash         TEXT NOT NULL,
    created_at   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_logs_ws_time ON audit_logs(workspace_id, created_at);

CREATE TABLE IF NOT EXISTS cmdb_model_groups (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    code         TEXT NOT NULL,
    name         TEXT NOT NULL,
    sort_order   INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL,
    UNIQUE(workspace_id, code)
);

CREATE TABLE IF NOT EXISTS cmdb_models (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    group_code   TEXT NOT NULL,
    code         TEXT NOT NULL,
    name         TEXT NOT NULL,
    icon         TEXT NOT NULL DEFAULT 'box',
    description  TEXT NOT NULL DEFAULT '',
    is_builtin   INTEGER NOT NULL DEFAULT 0,
    pos_x        DOUBLE PRECISION NOT NULL DEFAULT 100,
    pos_y        DOUBLE PRECISION NOT NULL DEFAULT 100,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    UNIQUE(workspace_id, code)
);

CREATE TABLE IF NOT EXISTS cmdb_model_attrs (
    id            TEXT PRIMARY KEY,
    workspace_id  TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    model_code    TEXT NOT NULL,
    attr_key      TEXT NOT NULL,
    name          TEXT NOT NULL,
    field_type    TEXT NOT NULL DEFAULT 'string',
    group_name    TEXT NOT NULL DEFAULT '基础属性',
    required      INTEGER NOT NULL DEFAULT 0,
    readonly      INTEGER NOT NULL DEFAULT 0,
    is_unique     INTEGER NOT NULL DEFAULT 0,
    default_value TEXT NOT NULL DEFAULT '',
    options_json  TEXT NOT NULL DEFAULT '[]',
    sort_order    INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL,
    UNIQUE(workspace_id, model_code, attr_key)
);

CREATE TABLE IF NOT EXISTS cmdb_model_relations (
    id            TEXT PRIMARY KEY,
    workspace_id  TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    source_model  TEXT NOT NULL,
    target_model  TEXT NOT NULL,
    relation_code TEXT NOT NULL,
    relation_name TEXT NOT NULL,
    cardinality   TEXT NOT NULL DEFAULT 'N:M',
    created_at    TEXT NOT NULL,
    UNIQUE(workspace_id, source_model, target_model, relation_code)
);

CREATE TABLE IF NOT EXISTS cmdb_ci_instances (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    model_code   TEXT NOT NULL,
    unique_key   TEXT NOT NULL,
    name         TEXT NOT NULL,
    env          TEXT NOT NULL DEFAULT 'prod',
    status       TEXT NOT NULL DEFAULT 'active',
    owner        TEXT NOT NULL DEFAULT '',
    tags_json    TEXT NOT NULL DEFAULT '[]',
    data_json    TEXT NOT NULL DEFAULT '{}',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    UNIQUE(workspace_id, model_code, unique_key)
);
CREATE INDEX IF NOT EXISTS idx_cmdb_ci_ws_model ON cmdb_ci_instances(workspace_id, model_code);

CREATE TABLE IF NOT EXISTS cmdb_host_endpoints (
    ci_id         TEXT PRIMARY KEY REFERENCES cmdb_ci_instances(id) ON DELETE CASCADE,
    workspace_id  TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    ip            TEXT NOT NULL,
    ssh_port      INTEGER NOT NULL DEFAULT 22,
    ssh_user      TEXT NOT NULL DEFAULT 'root',
    credential_id TEXT NOT NULL DEFAULT '',
    exec_mode     TEXT NOT NULL DEFAULT 'auto',
    agent_id      TEXT NOT NULL DEFAULT '',
    agent_status  TEXT NOT NULL DEFAULT 'OFFLINE',
    os_family     TEXT NOT NULL DEFAULT '',
    os_version    TEXT NOT NULL DEFAULT '',
    kernel        TEXT NOT NULL DEFAULT '',
    arch          TEXT NOT NULL DEFAULT 'amd64',
    cpu_cores     INTEGER NOT NULL DEFAULT 0,
    mem_mb        INTEGER NOT NULL DEFAULT 0,
    disk_gb       INTEGER NOT NULL DEFAULT 0,
    load_avg      TEXT NOT NULL DEFAULT '',
    cpu_usage_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
    mem_usage_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
    facts_json    TEXT NOT NULL DEFAULT '{}',
    last_seen_at  TEXT NOT NULL DEFAULT '',
    updated_at    TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_cmdb_host_ep_ws_ip ON cmdb_host_endpoints(workspace_id, ip);

CREATE TABLE IF NOT EXISTS cmdb_ci_relations (
    id            TEXT PRIMARY KEY,
    workspace_id  TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    source_ci_id  TEXT NOT NULL REFERENCES cmdb_ci_instances(id) ON DELETE CASCADE,
    target_ci_id  TEXT NOT NULL REFERENCES cmdb_ci_instances(id) ON DELETE CASCADE,
    relation_code TEXT NOT NULL,
    note          TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL,
    UNIQUE(workspace_id, source_ci_id, target_ci_id, relation_code)
);

CREATE TABLE IF NOT EXISTS cmdb_ci_history (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    ci_id        TEXT NOT NULL REFERENCES cmdb_ci_instances(id) ON DELETE CASCADE,
    action       TEXT NOT NULL,
    actor        TEXT NOT NULL,
    before_json  TEXT NOT NULL DEFAULT '{}',
    after_json   TEXT NOT NULL DEFAULT '{}',
    created_at   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_cmdb_ci_history_ci ON cmdb_ci_history(ci_id, created_at);

CREATE TABLE IF NOT EXISTS cmdb_service_templates (
    id                TEXT PRIMARY KEY,
    workspace_id      TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    code              TEXT NOT NULL,
    name              TEXT NOT NULL,
    category          TEXT NOT NULL DEFAULT 'application',
    ports_json        TEXT NOT NULL DEFAULT '[]',
    processes_json    TEXT NOT NULL DEFAULT '[]',
    env_defaults_json TEXT NOT NULL DEFAULT '{}',
    description       TEXT NOT NULL DEFAULT '',
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL,
    UNIQUE(workspace_id, code)
);

CREATE TABLE IF NOT EXISTS cmdb_topology_nodes (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    tree_type           TEXT NOT NULL,
    parent_id           TEXT NOT NULL DEFAULT '',
    node_type           TEXT NOT NULL,
    name                TEXT NOT NULL,
    code                TEXT NOT NULL,
    service_template_id TEXT NOT NULL DEFAULT '',
    labels_json         TEXT NOT NULL DEFAULT '{}',
    sort_order          INTEGER NOT NULL DEFAULT 0,
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_cmdb_topo_ws_tree ON cmdb_topology_nodes(workspace_id, tree_type);

CREATE TABLE IF NOT EXISTS cmdb_ci_topo_bindings (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    ci_id        TEXT NOT NULL REFERENCES cmdb_ci_instances(id) ON DELETE CASCADE,
    topo_node_id TEXT NOT NULL REFERENCES cmdb_topology_nodes(id) ON DELETE CASCADE,
    created_at   TEXT NOT NULL,
    UNIQUE(workspace_id, ci_id, topo_node_id)
);

CREATE TABLE IF NOT EXISTS cmdb_dynamic_groups (
    id              TEXT PRIMARY KEY,
    workspace_id    TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    model_code      TEXT NOT NULL DEFAULT 'm_host',
    conditions_json TEXT NOT NULL DEFAULT '[]',
    description     TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    UNIQUE(workspace_id, name)
);

CREATE TABLE IF NOT EXISTS dcim_racks (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    datacenter   TEXT NOT NULL,
    room         TEXT NOT NULL DEFAULT '',
    name         TEXT NOT NULL,
    u_height     INTEGER NOT NULL DEFAULT 42,
    power_kw     DOUBLE PRECISION NOT NULL DEFAULT 8.0,
    status       TEXT NOT NULL DEFAULT 'active',
    notes        TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    UNIQUE(workspace_id, datacenter, name)
);

CREATE TABLE IF NOT EXISTS dcim_rack_units (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    rack_id      TEXT NOT NULL REFERENCES dcim_racks(id) ON DELETE CASCADE,
    ci_id        TEXT NOT NULL DEFAULT '',
    device_name  TEXT NOT NULL,
    model_code   TEXT NOT NULL DEFAULT 'm_host',
    u_start      INTEGER NOT NULL,
    u_size       INTEGER NOT NULL DEFAULT 2,
    face         TEXT NOT NULL DEFAULT 'front',
    power_w      INTEGER NOT NULL DEFAULT 400,
    color_tag    TEXT NOT NULL DEFAULT 'primary',
    created_at   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS ipam_prefixes (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    cidr         TEXT NOT NULL,
    vlan_id      INTEGER NOT NULL DEFAULT 0,
    site         TEXT NOT NULL DEFAULT '',
    vrf          TEXT NOT NULL DEFAULT 'default',
    gateway      TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'active',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    UNIQUE(workspace_id, cidr)
);

CREATE TABLE IF NOT EXISTS ipam_addresses (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    prefix_id    TEXT NOT NULL REFERENCES ipam_prefixes(id) ON DELETE CASCADE,
    ip           TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'active',
    ci_id        TEXT NOT NULL DEFAULT '',
    dns_name     TEXT NOT NULL DEFAULT '',
    mac_addr     TEXT NOT NULL DEFAULT '',
    owner        TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    UNIQUE(workspace_id, ip)
);

CREATE TABLE IF NOT EXISTS cfg_namespaces (
    id             TEXT PRIMARY KEY,
    workspace_id   TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    env            TEXT NOT NULL,
    app_code       TEXT NOT NULL,
    name           TEXT NOT NULL,
    format         TEXT NOT NULL DEFAULT 'kv',
    description    TEXT NOT NULL DEFAULT '',
    active_version INTEGER NOT NULL DEFAULT 0,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    UNIQUE(workspace_id, env, app_code, name)
);

CREATE TABLE IF NOT EXISTS cfg_items (
    id           TEXT PRIMARY KEY,
    namespace_id TEXT NOT NULL REFERENCES cfg_namespaces(id) ON DELETE CASCADE,
    item_key     TEXT NOT NULL,
    value_plain  TEXT NOT NULL DEFAULT '',
    value_enc    TEXT NOT NULL DEFAULT '',
    is_secret    INTEGER NOT NULL DEFAULT 0,
    value_type   TEXT NOT NULL DEFAULT 'string',
    description  TEXT NOT NULL DEFAULT '',
    updated_by   TEXT NOT NULL DEFAULT '',
    updated_at   TEXT NOT NULL,
    UNIQUE(namespace_id, item_key)
);

CREATE TABLE IF NOT EXISTS cfg_releases (
    id            TEXT PRIMARY KEY,
    namespace_id  TEXT NOT NULL REFERENCES cfg_namespaces(id) ON DELETE CASCADE,
    version       INTEGER NOT NULL,
    etag          TEXT NOT NULL,
    comment       TEXT NOT NULL DEFAULT '',
    snapshot_json TEXT NOT NULL,
    released_by   TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    UNIQUE(namespace_id, version)
);

CREATE TABLE IF NOT EXISTS artifact_records (
    id                TEXT PRIMARY KEY,
    workspace_id      TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name              TEXT NOT NULL,
    version           TEXT NOT NULL DEFAULT '1.0.0',
    kind              TEXT NOT NULL DEFAULT 'binary',
    arch              TEXT NOT NULL DEFAULT 'amd64',
    sha256            TEXT NOT NULL,
    size_bytes        BIGINT NOT NULL DEFAULT 0,
    storage_path      TEXT NOT NULL,
    signature_ed25519 TEXT NOT NULL DEFAULT '',
    oci_ref           TEXT NOT NULL DEFAULT '',
    metadata_json     TEXT NOT NULL DEFAULT '{}',
    uploaded_by       TEXT NOT NULL DEFAULT '',
    created_at        TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_artifact_ws_sha ON artifact_records(workspace_id, sha256);

CREATE TABLE IF NOT EXISTS delivery_plugins (
    id            TEXT PRIMARY KEY,
    workspace_id  TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    code          TEXT NOT NULL,
    name          TEXT NOT NULL,
    category      TEXT NOT NULL DEFAULT 'middleware',
    version       TEXT NOT NULL DEFAULT '1.0.0',
    runtime       TEXT NOT NULL DEFAULT 'compose',
    description   TEXT NOT NULL DEFAULT '',
    is_builtin    INTEGER NOT NULL DEFAULT 0,
    manifest_yaml TEXT NOT NULL,
    schema_json   TEXT NOT NULL DEFAULT '[]',
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    UNIQUE(workspace_id, code)
);

CREATE TABLE IF NOT EXISTS delivery_releases (
    id                  TEXT PRIMARY KEY,
    workspace_id        TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name                TEXT NOT NULL,
    env                 TEXT NOT NULL DEFAULT 'prod',
    mode                TEXT NOT NULL DEFAULT 'DAG',
    status              TEXT NOT NULL DEFAULT 'DRAFT',
    global_params_json  TEXT NOT NULL DEFAULT '{}',
    service_params_json TEXT NOT NULL DEFAULT '{}',
    plan_json           TEXT NOT NULL DEFAULT '{}',
    created_by          TEXT NOT NULL DEFAULT '',
    started_at          TEXT NOT NULL DEFAULT '',
    finished_at         TEXT NOT NULL DEFAULT '',
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS delivery_release_items (
    id                    TEXT PRIMARY KEY,
    release_id            TEXT NOT NULL REFERENCES delivery_releases(id) ON DELETE CASCADE,
    node_key              TEXT NOT NULL,
    plugin_code           TEXT NOT NULL,
    service_name          TEXT NOT NULL,
    target_host_ci_id     TEXT NOT NULL DEFAULT '',
    batch_index           INTEGER NOT NULL DEFAULT 0,
    depends_on_json       TEXT NOT NULL DEFAULT '[]',
    pause_after           INTEGER NOT NULL DEFAULT 0,
    spec_params_json      TEXT NOT NULL DEFAULT '{}',
    effective_params_json TEXT NOT NULL DEFAULT '{}',
    k8s_spec_json         TEXT NOT NULL DEFAULT '{}',
    dial_url              TEXT NOT NULL DEFAULT '',
    status                TEXT NOT NULL DEFAULT 'PENDING',
    synced_ci_id          TEXT NOT NULL DEFAULT '',
    summary               TEXT NOT NULL DEFAULT '',
    started_at            TEXT NOT NULL DEFAULT '',
    finished_at           TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS exec_enroll_tokens (
    id               TEXT PRIMARY KEY,
    workspace_id     TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    label            TEXT NOT NULL DEFAULT '',
    zone             TEXT NOT NULL DEFAULT 'default',
    token_hash       TEXT NOT NULL UNIQUE,
    expires_at       TEXT NOT NULL,
    used_at          TEXT NOT NULL DEFAULT '',
    used_by_agent_id TEXT NOT NULL DEFAULT '',
    created_by       TEXT NOT NULL DEFAULT '',
    created_at       TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS exec_agents (
    id                TEXT PRIMARY KEY,
    workspace_id      TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    host_ci_id        TEXT NOT NULL DEFAULT '',
    hostname          TEXT NOT NULL,
    ip                TEXT NOT NULL DEFAULT '',
    zone              TEXT NOT NULL DEFAULT 'default',
    os                TEXT NOT NULL DEFAULT 'linux',
    arch              TEXT NOT NULL DEFAULT 'amd64',
    version           TEXT NOT NULL DEFAULT '2.0.0',
    token_hash        TEXT NOT NULL UNIQUE,
    status            TEXT NOT NULL DEFAULT 'ONLINE',
    cpu_cores         INTEGER NOT NULL DEFAULT 0,
    mem_mb            INTEGER NOT NULL DEFAULT 0,
    load_avg          TEXT NOT NULL DEFAULT '',
    cpu_usage_pct     DOUBLE PRECISION NOT NULL DEFAULT 0,
    mem_usage_pct     DOUBLE PRECISION NOT NULL DEFAULT 0,
    last_heartbeat_at TEXT NOT NULL,
    enrolled_at       TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS exec_jobs (
    id              TEXT PRIMARY KEY,
    workspace_id    TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    template_code   TEXT NOT NULL DEFAULT '',
    command         TEXT NOT NULL,
    artifact_id     TEXT NOT NULL DEFAULT '',
    remote_dest     TEXT NOT NULL DEFAULT '',
    target_mode     TEXT NOT NULL DEFAULT 'hosts',
    target_ref_json TEXT NOT NULL DEFAULT '[]',
    status          TEXT NOT NULL DEFAULT 'PENDING',
    created_by      TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL,
    finished_at     TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS exec_job_runs (
    id          TEXT PRIMARY KEY,
    job_id      TEXT NOT NULL REFERENCES exec_jobs(id) ON DELETE CASCADE,
    host_ci_id  TEXT NOT NULL,
    host_name   TEXT NOT NULL DEFAULT '',
    host_ip     TEXT NOT NULL DEFAULT '',
    transport   TEXT NOT NULL DEFAULT 'ssh',
    status      TEXT NOT NULL DEFAULT 'PENDING',
    exit_code   INTEGER NOT NULL DEFAULT 0,
    output      TEXT NOT NULL DEFAULT '',
    duration_ms BIGINT NOT NULL DEFAULT 0,
    started_at  TEXT NOT NULL DEFAULT '',
    finished_at TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE IF EXISTS exec_job_runs;
DROP TABLE IF EXISTS exec_jobs;
DROP TABLE IF EXISTS exec_agents;
DROP TABLE IF EXISTS exec_enroll_tokens;
DROP TABLE IF EXISTS delivery_release_items;
DROP TABLE IF EXISTS delivery_releases;
DROP TABLE IF EXISTS delivery_plugins;
DROP TABLE IF EXISTS artifact_records;
DROP TABLE IF EXISTS cfg_releases;
DROP TABLE IF EXISTS cfg_items;
DROP TABLE IF EXISTS cfg_namespaces;
DROP TABLE IF EXISTS ipam_addresses;
DROP TABLE IF EXISTS ipam_prefixes;
DROP TABLE IF EXISTS dcim_rack_units;
DROP TABLE IF EXISTS dcim_racks;
DROP TABLE IF EXISTS cmdb_dynamic_groups;
DROP TABLE IF EXISTS cmdb_ci_topo_bindings;
DROP TABLE IF EXISTS cmdb_topology_nodes;
DROP TABLE IF EXISTS cmdb_service_templates;
DROP TABLE IF EXISTS cmdb_ci_history;
DROP TABLE IF EXISTS cmdb_ci_relations;
DROP TABLE IF EXISTS cmdb_host_endpoints;
DROP TABLE IF EXISTS cmdb_ci_instances;
DROP TABLE IF EXISTS cmdb_model_relations;
DROP TABLE IF EXISTS cmdb_model_attrs;
DROP TABLE IF EXISTS cmdb_models;
DROP TABLE IF EXISTS cmdb_model_groups;
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS credentials;
DROP TABLE IF EXISTS api_tokens;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS workspaces;
