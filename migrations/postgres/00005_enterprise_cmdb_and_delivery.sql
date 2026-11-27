-- +goose Up
-- PostgreSQL dialect: Enterprise Dynamic Meta-Model CMDB, DCIM/IPAM, Versioned Config Center,
-- Declarative Plugin Packages, Batch/M1/M2/M3 Delivery Engine & Zero-Trust Agent Fleet

CREATE TABLE IF NOT EXISTS cmdb_model_groups (
    id          VARCHAR(48) PRIMARY KEY,
    project_id  VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    code        VARCHAR(64) NOT NULL,
    name        VARCHAR(128) NOT NULL,
    sort_order  INTEGER NOT NULL DEFAULT 0,
    is_builtin  BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, code)
);
CREATE INDEX IF NOT EXISTS idx_cmdb_mgrp_proj ON cmdb_model_groups(project_id);

CREATE TABLE IF NOT EXISTS cmdb_models (
    id                   VARCHAR(48) PRIMARY KEY,
    project_id           VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    group_code           VARCHAR(64) NOT NULL,
    code                 VARCHAR(64) NOT NULL,
    name                 VARCHAR(128) NOT NULL,
    icon                 VARCHAR(48) NOT NULL DEFAULT 'Box',
    description          TEXT NOT NULL DEFAULT '',
    pos_x                DOUBLE PRECISION NOT NULL DEFAULT 0,
    pos_y                DOUBLE PRECISION NOT NULL DEFAULT 0,
    is_builtin           BOOLEAN NOT NULL DEFAULT FALSE,
    is_mainline          BOOLEAN NOT NULL DEFAULT FALSE,
    mainline_parent_code VARCHAR(64) NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, code)
);
CREATE INDEX IF NOT EXISTS idx_cmdb_models_proj ON cmdb_models(project_id);

CREATE TABLE IF NOT EXISTS cmdb_model_attributes (
    id                  VARCHAR(48) PRIMARY KEY,
    project_id          VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    model_code          VARCHAR(64) NOT NULL,
    attr_group          VARCHAR(64) NOT NULL DEFAULT '基础属性',
    prop_id             VARCHAR(64) NOT NULL,
    prop_name           VARCHAR(128) NOT NULL,
    field_type          VARCHAR(24) NOT NULL DEFAULT 'string',
    unit                VARCHAR(32) NOT NULL DEFAULT '',
    required            BOOLEAN NOT NULL DEFAULT FALSE,
    readonly            BOOLEAN NOT NULL DEFAULT FALSE,
    is_unique           BOOLEAN NOT NULL DEFAULT FALSE,
    display             BOOLEAN NOT NULL DEFAULT TRUE,
    enum_options_json   JSONB NOT NULL DEFAULT '[]'::jsonb,
    default_value       TEXT NOT NULL DEFAULT '',
    source_kind         VARCHAR(24) NOT NULL DEFAULT 'manual',
    collector_plugin_id VARCHAR(64) NOT NULL DEFAULT '',
    sort_order          INTEGER NOT NULL DEFAULT 0,
    UNIQUE (project_id, model_code, prop_id)
);
CREATE INDEX IF NOT EXISTS idx_cmdb_attr_model ON cmdb_model_attributes(project_id, model_code);

CREATE TABLE IF NOT EXISTS cmdb_model_relations (
    id            VARCHAR(48) PRIMARY KEY,
    project_id    VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    source_model  VARCHAR(64) NOT NULL,
    target_model  VARCHAR(64) NOT NULL,
    relation_type VARCHAR(48) NOT NULL,
    label         VARCHAR(64) NOT NULL DEFAULT '',
    mapping_kind  VARCHAR(16) NOT NULL DEFAULT '1:N',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, source_model, target_model, relation_type)
);
CREATE INDEX IF NOT EXISTS idx_cmdb_mrel_proj ON cmdb_model_relations(project_id);

CREATE TABLE IF NOT EXISTS cmdb_ci_instances (
    id          VARCHAR(48) PRIMARY KEY,
    project_id  VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    model_code  VARCHAR(64) NOT NULL,
    name        VARCHAR(128) NOT NULL,
    unique_key  VARCHAR(128) NOT NULL DEFAULT '',
    status      VARCHAR(32) NOT NULL DEFAULT 'active',
    env         VARCHAR(32) NOT NULL DEFAULT 'prod',
    owner       VARCHAR(64) NOT NULL DEFAULT '',
    attrs_json  JSONB NOT NULL DEFAULT '{}'::jsonb,
    version     INTEGER NOT NULL DEFAULT 1,
    created_by  VARCHAR(128) NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_cmdb_ci_proj_model ON cmdb_ci_instances(project_id, model_code);
CREATE INDEX IF NOT EXISTS idx_cmdb_ci_attrs_gin ON cmdb_ci_instances USING GIN(attrs_json);
CREATE UNIQUE INDEX IF NOT EXISTS idx_cmdb_ci_unique_key ON cmdb_ci_instances(project_id, model_code, unique_key) WHERE unique_key <> '';

CREATE TABLE IF NOT EXISTS cmdb_ci_relations (
    id            VARCHAR(48) PRIMARY KEY,
    project_id    VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    source_ci_id  VARCHAR(48) NOT NULL REFERENCES cmdb_ci_instances(id) ON DELETE CASCADE,
    target_ci_id  VARCHAR(48) NOT NULL REFERENCES cmdb_ci_instances(id) ON DELETE CASCADE,
    relation_type VARCHAR(48) NOT NULL,
    label         VARCHAR(64) NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, source_ci_id, target_ci_id, relation_type)
);
CREATE INDEX IF NOT EXISTS idx_cmdb_cirel_src ON cmdb_ci_relations(project_id, source_ci_id);
CREATE INDEX IF NOT EXISTS idx_cmdb_cirel_tgt ON cmdb_ci_relations(project_id, target_ci_id);

CREATE TABLE IF NOT EXISTS cmdb_ci_history (
    id          VARCHAR(48) PRIMARY KEY,
    project_id  VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    ci_id       VARCHAR(48) NOT NULL,
    model_code  VARCHAR(64) NOT NULL,
    action      VARCHAR(24) NOT NULL,
    before_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    after_json  JSONB NOT NULL DEFAULT '{}'::jsonb,
    actor       VARCHAR(128) NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_cmdb_cihist_ci ON cmdb_ci_history(project_id, ci_id, created_at);

CREATE TABLE IF NOT EXISTS cmdb_service_templates (
    id                  VARCHAR(48) PRIMARY KEY,
    project_id          VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name                VARCHAR(128) NOT NULL,
    category            VARCHAR(64) NOT NULL DEFAULT '业务服务',
    description         TEXT NOT NULL DEFAULT '',
    processes_json      JSONB NOT NULL DEFAULT '[]'::jsonb,
    ports_json          JSONB NOT NULL DEFAULT '[]'::jsonb,
    default_params_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, name)
);

CREATE TABLE IF NOT EXISTS cmdb_topology_nodes (
    id                  VARCHAR(48) PRIMARY KEY,
    project_id          VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    topo_kind           VARCHAR(24) NOT NULL CHECK (topo_kind IN ('business','resource')),
    parent_id           VARCHAR(48) NOT NULL DEFAULT '',
    model_code          VARCHAR(64) NOT NULL,
    name                VARCHAR(128) NOT NULL,
    service_template_id VARCHAR(48) NOT NULL DEFAULT '',
    metadata_json       JSONB NOT NULL DEFAULT '{}'::jsonb,
    sort_order          INTEGER NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_cmdb_topo_proj ON cmdb_topology_nodes(project_id, topo_kind, parent_id);

CREATE TABLE IF NOT EXISTS cmdb_host_bindings (
    id           VARCHAR(48) PRIMARY KEY,
    project_id   VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    host_id      VARCHAR(48) NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
    topo_node_id VARCHAR(48) NOT NULL REFERENCES cmdb_topology_nodes(id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, host_id, topo_node_id)
);
CREATE INDEX IF NOT EXISTS idx_cmdb_hbind_node ON cmdb_host_bindings(project_id, topo_node_id);

CREATE TABLE IF NOT EXISTS dcim_racks (
    id          VARCHAR(48) PRIMARY KEY,
    project_id  VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    site_name   VARCHAR(128) NOT NULL,
    room_name   VARCHAR(128) NOT NULL DEFAULT '主数据机房',
    name        VARCHAR(64) NOT NULL,
    u_height    INTEGER NOT NULL DEFAULT 42 CHECK (u_height BETWEEN 1 AND 60),
    power_kw    DOUBLE PRECISION NOT NULL DEFAULT 8.0,
    status      VARCHAR(24) NOT NULL DEFAULT 'active',
    notes       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, site_name, name)
);
CREATE INDEX IF NOT EXISTS idx_dcim_rack_proj ON dcim_racks(project_id);

CREATE TABLE IF NOT EXISTS dcim_rack_units (
    id          VARCHAR(48) PRIMARY KEY,
    project_id  VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    rack_id     VARCHAR(48) NOT NULL REFERENCES dcim_racks(id) ON DELETE CASCADE,
    ci_id       VARCHAR(48) NOT NULL DEFAULT '',
    device_name VARCHAR(128) NOT NULL,
    device_role VARCHAR(48) NOT NULL DEFAULT 'server',
    start_u     INTEGER NOT NULL CHECK (start_u >= 1),
    u_size      INTEGER NOT NULL DEFAULT 2 CHECK (u_size >= 1),
    power_w     INTEGER NOT NULL DEFAULT 450,
    status      VARCHAR(24) NOT NULL DEFAULT 'active',
    color_tag   VARCHAR(24) NOT NULL DEFAULT 'primary',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_dcim_unit_rack ON dcim_rack_units(project_id, rack_id);

CREATE TABLE IF NOT EXISTS ipam_prefixes (
    id          VARCHAR(48) PRIMARY KEY,
    project_id  VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    cidr        VARCHAR(64) NOT NULL,
    vlan_id     INTEGER NOT NULL DEFAULT 0,
    vrf         VARCHAR(64) NOT NULL DEFAULT 'default',
    site_name   VARCHAR(128) NOT NULL DEFAULT '',
    gateway     VARCHAR(64) NOT NULL DEFAULT '',
    role        VARCHAR(48) NOT NULL DEFAULT 'business',
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, vrf, cidr)
);
CREATE INDEX IF NOT EXISTS idx_ipam_prefix_proj ON ipam_prefixes(project_id);

CREATE TABLE IF NOT EXISTS ipam_addresses (
    id          VARCHAR(48) PRIMARY KEY,
    project_id  VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    prefix_id   VARCHAR(48) NOT NULL REFERENCES ipam_prefixes(id) ON DELETE CASCADE,
    ip_address  VARCHAR(64) NOT NULL,
    status      VARCHAR(24) NOT NULL DEFAULT 'active' CHECK (status IN ('active','reserved','dhcp','deprecated')),
    ci_id       VARCHAR(48) NOT NULL DEFAULT '',
    host_id     VARCHAR(48) NOT NULL DEFAULT '',
    hostname    VARCHAR(128) NOT NULL DEFAULT '',
    mac_address VARCHAR(32) NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, prefix_id, ip_address)
);
CREATE INDEX IF NOT EXISTS idx_ipam_addr_prefix ON ipam_addresses(project_id, prefix_id);

CREATE TABLE IF NOT EXISTS cfg_namespaces (
    id                  VARCHAR(48) PRIMARY KEY,
    project_id          VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    env                 VARCHAR(16) NOT NULL CHECK (env IN ('dev','sit','uat','beta','prod')),
    name                VARCHAR(128) NOT NULL,
    format_kind         VARCHAR(16) NOT NULL DEFAULT 'kv' CHECK (format_kind IN ('kv','yaml','properties')),
    description         TEXT NOT NULL DEFAULT '',
    current_release_ver INTEGER NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, env, name)
);
CREATE INDEX IF NOT EXISTS idx_cfg_ns_proj_env ON cfg_namespaces(project_id, env);

CREATE TABLE IF NOT EXISTS cfg_items (
    id           VARCHAR(48) PRIMARY KEY,
    namespace_id VARCHAR(48) NOT NULL REFERENCES cfg_namespaces(id) ON DELETE CASCADE,
    item_key     VARCHAR(128) NOT NULL,
    item_type    VARCHAR(16) NOT NULL DEFAULT 'string' CHECK (item_type IN ('string','json','yaml','secret')),
    value_plain  TEXT NOT NULL DEFAULT '',
    value_cipher TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    version      INTEGER NOT NULL DEFAULT 1,
    updated_by   VARCHAR(128) NOT NULL DEFAULT '',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (namespace_id, item_key)
);
CREATE INDEX IF NOT EXISTS idx_cfg_items_ns ON cfg_items(namespace_id);

CREATE TABLE IF NOT EXISTS cfg_releases (
    id            VARCHAR(48) PRIMARY KEY,
    namespace_id  VARCHAR(48) NOT NULL REFERENCES cfg_namespaces(id) ON DELETE CASCADE,
    release_ver   INTEGER NOT NULL,
    etag          VARCHAR(128) NOT NULL,
    snapshot_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    comment       TEXT NOT NULL DEFAULT '',
    orc_ticket_no VARCHAR(64) NOT NULL DEFAULT '',
    published_by  VARCHAR(128) NOT NULL DEFAULT '',
    published_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (namespace_id, release_ver)
);
CREATE INDEX IF NOT EXISTS idx_cfg_rel_ns ON cfg_releases(namespace_id, release_ver);

CREATE TABLE IF NOT EXISTS declarative_plugins (
    id                VARCHAR(48) PRIMARY KEY,
    project_id        VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name              VARCHAR(64) NOT NULL,
    display_name      VARCHAR(128) NOT NULL,
    category          VARCHAR(64) NOT NULL DEFAULT 'custom',
    version           VARCHAR(32) NOT NULL DEFAULT '1.0.0',
    icon              VARCHAR(48) NOT NULL DEFAULT 'Package',
    description       TEXT NOT NULL DEFAULT '',
    targets_json      JSONB NOT NULL DEFAULT '["host-container"]'::jsonb,
    manifest_yaml     TEXT NOT NULL,
    param_schema_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    is_enabled        BOOLEAN NOT NULL DEFAULT TRUE,
    created_by        VARCHAR(128) NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, name)
);
CREATE INDEX IF NOT EXISTS idx_decl_plug_proj ON declarative_plugins(project_id);

CREATE TABLE IF NOT EXISTS delivery_releases (
    id                  VARCHAR(48) PRIMARY KEY,
    project_id          VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name                VARCHAR(128) NOT NULL,
    env                 VARCHAR(16) NOT NULL DEFAULT 'prod',
    schedule_mode       VARCHAR(16) NOT NULL DEFAULT 'BATCH' CHECK (schedule_mode IN ('BATCH','M1','M2','M3')),
    status              VARCHAR(24) NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','RUNNING','PAUSED','SUCCEEDED','FAILED','ROLLED_BACK','CANCELLED')),
    config_namespace_id VARCHAR(48) NOT NULL DEFAULT '',
    global_params_json  JSONB NOT NULL DEFAULT '{}'::jsonb,
    spec_matrix_json    JSONB NOT NULL DEFAULT '{}'::jsonb,
    current_batch       INTEGER NOT NULL DEFAULT 0,
    created_by          VARCHAR(128) NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_deliv_rel_proj ON delivery_releases(project_id, created_at);

CREATE TABLE IF NOT EXISTS delivery_release_items (
    id                    VARCHAR(48) PRIMARY KEY,
    release_id            VARCHAR(48) NOT NULL REFERENCES delivery_releases(id) ON DELETE CASCADE,
    batch_no              INTEGER NOT NULL DEFAULT 0,
    seq_no                INTEGER NOT NULL DEFAULT 0,
    service_name          VARCHAR(128) NOT NULL,
    plugin_name           VARCHAR(64) NOT NULL DEFAULT '',
    target_mode           VARCHAR(32) NOT NULL DEFAULT 'host-container',
    host_ids_json         JSONB NOT NULL DEFAULT '[]'::jsonb,
    service_params_json   JSONB NOT NULL DEFAULT '{}'::jsonb,
    effective_params_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    pause_after           BOOLEAN NOT NULL DEFAULT FALSE,
    stage                 VARCHAR(24) NOT NULL DEFAULT 'PENDING',
    status                VARCHAR(24) NOT NULL DEFAULT 'PENDING',
    probe_url             TEXT NOT NULL DEFAULT '',
    probe_port            INTEGER NOT NULL DEFAULT 0,
    last_output           TEXT NOT NULL DEFAULT '',
    started_at            TIMESTAMPTZ,
    finished_at           TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_deliv_items_rel ON delivery_release_items(release_id, batch_no, seq_no);

CREATE TABLE IF NOT EXISTS exec_enroll_tokens (
    id           VARCHAR(48) PRIMARY KEY,
    project_id   VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    zone         VARCHAR(64) NOT NULL DEFAULT 'default',
    token_sha256 VARCHAR(64) NOT NULL UNIQUE,
    max_uses     INTEGER NOT NULL DEFAULT 1,
    used_count   INTEGER NOT NULL DEFAULT 0,
    expires_at   TIMESTAMPTZ NOT NULL,
    created_by   VARCHAR(128) NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_exec_tok_proj ON exec_enroll_tokens(project_id);

CREATE TABLE IF NOT EXISTS exec_agents (
    id                VARCHAR(48) PRIMARY KEY,
    project_id        VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    host_id           VARCHAR(48) NOT NULL DEFAULT '',
    hostname          VARCHAR(128) NOT NULL,
    ip_address        VARCHAR(64) NOT NULL DEFAULT '',
    zone              VARCHAR(64) NOT NULL DEFAULT 'default',
    os_arch           VARCHAR(32) NOT NULL DEFAULT 'amd64',
    os_info           VARCHAR(128) NOT NULL DEFAULT '',
    agent_version     VARCHAR(32) NOT NULL DEFAULT '1.0.0',
    status            VARCHAR(24) NOT NULL DEFAULT 'ONLINE' CHECK (status IN ('ONLINE','OFFLINE','REVOKED')),
    auth_token_sha256 VARCHAR(64) NOT NULL UNIQUE,
    capabilities_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    metrics_json      JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    enrolled_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_exec_agents_proj ON exec_agents(project_id, zone, status);

CREATE TABLE IF NOT EXISTS exec_agent_tasks (
    id           VARCHAR(48) PRIMARY KEY,
    project_id   VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    agent_id     VARCHAR(48) NOT NULL REFERENCES exec_agents(id) ON DELETE CASCADE,
    kind         VARCHAR(32) NOT NULL,
    payload_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    status       VARCHAR(24) NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','DISPATCHED','SUCCEEDED','FAILED','CANCELLED')),
    exit_code    INTEGER NOT NULL DEFAULT 0,
    stdout       TEXT NOT NULL DEFAULT '',
    stderr       TEXT NOT NULL DEFAULT '',
    outputs_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_exec_atasks_agent ON exec_agent_tasks(agent_id, status, created_at);
