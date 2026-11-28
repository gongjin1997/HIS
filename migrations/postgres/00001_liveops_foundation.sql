-- +goose Up
-- LiveOps Enterprise Foundation Schema (PostgreSQL 16 Dialect + FORCE ROW LEVEL SECURITY)

CREATE TABLE iam_tenants (
    tenant_id   TEXT PRIMARY KEY,
    code        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE TABLE iam_apps (
    tenant_id   TEXT NOT NULL,
    app_id      TEXT NOT NULL,
    code        TEXT NOT NULL,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id),
    UNIQUE (tenant_id, code),
    FOREIGN KEY (tenant_id) REFERENCES iam_tenants(tenant_id) ON DELETE CASCADE
);

CREATE TABLE iam_app_keys (
    tenant_id    TEXT NOT NULL,
    app_id       TEXT NOT NULL,
    key_id       TEXT NOT NULL,
    key_prefix   TEXT NOT NULL,
    key_hash     TEXT NOT NULL UNIQUE,
    status       TEXT NOT NULL DEFAULT 'ACTIVE',
    grace_until  TEXT NOT NULL DEFAULT '',
    last_used_at TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, key_id),
    FOREIGN KEY (tenant_id, app_id) REFERENCES iam_apps(tenant_id, app_id) ON DELETE CASCADE
);

CREATE TABLE iam_projects (
    tenant_id          TEXT NOT NULL,
    app_id             TEXT NOT NULL,
    project_id         TEXT NOT NULL,
    code               TEXT NOT NULL,
    name               TEXT NOT NULL,
    description        TEXT NOT NULL DEFAULT '',
    permission_version BIGINT NOT NULL DEFAULT 1,
    status             TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, project_id),
    UNIQUE (tenant_id, app_id, code),
    FOREIGN KEY (tenant_id, app_id) REFERENCES iam_apps(tenant_id, app_id) ON DELETE CASCADE
);

CREATE TABLE iam_users (
    user_id         TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL,
    username        TEXT NOT NULL UNIQUE,
    display_name    TEXT NOT NULL,
    email           TEXT NOT NULL DEFAULT '',
    password_hash   TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'ACTIVE',
    failed_attempts BIGINT NOT NULL DEFAULT 0,
    locked_until    TEXT NOT NULL DEFAULT '',
    last_login_at   TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    FOREIGN KEY (tenant_id) REFERENCES iam_tenants(tenant_id) ON DELETE CASCADE
);

CREATE TABLE iam_roles (
    role_id     TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL,
    code        TEXT NOT NULL,
    name        TEXT NOT NULL,
    scope_level TEXT NOT NULL DEFAULT 'WORKSPACE',
    description TEXT NOT NULL DEFAULT '',
    is_builtin  INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    UNIQUE (tenant_id, code),
    FOREIGN KEY (tenant_id) REFERENCES iam_tenants(tenant_id) ON DELETE CASCADE
);

CREATE TABLE iam_perms (
    perm_code   TEXT PRIMARY KEY,
    domain      TEXT NOT NULL,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT ''
);

CREATE TABLE iam_role_perms (
    role_id   TEXT NOT NULL,
    perm_code TEXT NOT NULL,
    PRIMARY KEY (role_id, perm_code),
    FOREIGN KEY (role_id) REFERENCES iam_roles(role_id) ON DELETE CASCADE,
    FOREIGN KEY (perm_code) REFERENCES iam_perms(perm_code) ON DELETE CASCADE
);

CREATE TABLE iam_user_roles (
    user_id    TEXT NOT NULL,
    role_id    TEXT NOT NULL,
    tenant_id  TEXT NOT NULL,
    app_id     TEXT NOT NULL DEFAULT '',
    project_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    PRIMARY KEY (user_id, role_id, tenant_id, app_id, project_id),
    FOREIGN KEY (user_id) REFERENCES iam_users(user_id) ON DELETE CASCADE,
    FOREIGN KEY (role_id) REFERENCES iam_roles(role_id) ON DELETE CASCADE
);

CREATE TABLE iam_project_roles (
    tenant_id   TEXT NOT NULL,
    app_id      TEXT NOT NULL,
    project_id  TEXT NOT NULL,
    role_id     TEXT NOT NULL,
    role_name   TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, project_id, role_id),
    FOREIGN KEY (tenant_id, app_id, project_id) REFERENCES iam_projects(tenant_id, app_id, project_id) ON DELETE CASCADE
);

CREATE TABLE iam_project_role_perms (
    tenant_id  TEXT NOT NULL,
    app_id     TEXT NOT NULL,
    project_id TEXT NOT NULL,
    role_id    TEXT NOT NULL,
    perm_code  TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, project_id, role_id, perm_code),
    FOREIGN KEY (tenant_id, app_id, project_id, role_id) REFERENCES iam_project_roles(tenant_id, app_id, project_id, role_id) ON DELETE CASCADE
);

CREATE TABLE iam_route_policies (
    policy_id     TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL,
    method        TEXT NOT NULL,
    path_pattern  TEXT NOT NULL,
    required_perm TEXT NOT NULL,
    scope_level   TEXT NOT NULL DEFAULT 'PROJECT',
    enabled       INTEGER NOT NULL DEFAULT 1,
    description   TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    UNIQUE (tenant_id, method, path_pattern),
    FOREIGN KEY (tenant_id) REFERENCES iam_tenants(tenant_id) ON DELETE CASCADE
);

CREATE TABLE iam_scope_versions (
    tenant_id     TEXT NOT NULL,
    app_id        TEXT NOT NULL,
    scope_version BIGINT NOT NULL DEFAULT 1,
    updated_at    TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id),
    FOREIGN KEY (tenant_id, app_id) REFERENCES iam_apps(tenant_id, app_id) ON DELETE CASCADE
);

CREATE TABLE iam_audit_logs (
    audit_id      TEXT PRIMARY KEY,
    seq           BIGINT NOT NULL UNIQUE,
    tenant_id     TEXT NOT NULL,
    app_id        TEXT NOT NULL DEFAULT '',
    actor_id      TEXT NOT NULL,
    actor_name    TEXT NOT NULL,
    action        TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id   TEXT NOT NULL,
    outcome       TEXT NOT NULL,
    request_id    TEXT NOT NULL DEFAULT '',
    source_ip     TEXT NOT NULL DEFAULT '',
    detail_json   TEXT NOT NULL DEFAULT '{}',
    prev_hash     TEXT NOT NULL,
    signature     TEXT NOT NULL,
    created_at    TEXT NOT NULL
);
CREATE INDEX idx_iam_audit_scope ON iam_audit_logs (tenant_id, app_id, seq DESC);

CREATE TABLE assets_model_groups (
    tenant_id  TEXT NOT NULL,
    app_id     TEXT NOT NULL,
    group_id   TEXT NOT NULL,
    name       TEXT NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, group_id),
    FOREIGN KEY (tenant_id, app_id) REFERENCES iam_apps(tenant_id, app_id) ON DELETE CASCADE
);

CREATE TABLE assets_models (
    tenant_id   TEXT NOT NULL,
    app_id      TEXT NOT NULL,
    model_id    TEXT NOT NULL,
    group_id    TEXT NOT NULL,
    code        TEXT NOT NULL,
    name        TEXT NOT NULL,
    icon        TEXT NOT NULL DEFAULT 'box',
    is_builtin  INTEGER NOT NULL DEFAULT 0,
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, model_id),
    UNIQUE (tenant_id, app_id, code),
    FOREIGN KEY (tenant_id, app_id, group_id) REFERENCES assets_model_groups(tenant_id, app_id, group_id)
);

CREATE TABLE assets_model_fields (
    tenant_id        TEXT NOT NULL,
    app_id           TEXT NOT NULL,
    model_id         TEXT NOT NULL,
    field_key        TEXT NOT NULL,
    name             TEXT NOT NULL,
    field_type       TEXT NOT NULL DEFAULT 'string',
    required         INTEGER NOT NULL DEFAULT 0,
    is_unique        INTEGER NOT NULL DEFAULT 0,
    enum_values_json TEXT NOT NULL DEFAULT '[]',
    default_value    TEXT NOT NULL DEFAULT '',
    sort_order       INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, app_id, model_id, field_key),
    FOREIGN KEY (tenant_id, app_id, model_id) REFERENCES assets_models(tenant_id, app_id, model_id) ON DELETE CASCADE
);

CREATE TABLE assets_relation_types (
    tenant_id       TEXT NOT NULL,
    app_id          TEXT NOT NULL,
    rel_type_id     TEXT NOT NULL,
    code            TEXT NOT NULL,
    name            TEXT NOT NULL,
    source_model_id TEXT NOT NULL DEFAULT '',
    target_model_id TEXT NOT NULL DEFAULT '',
    cardinality     TEXT NOT NULL DEFAULT '1:N',
    created_at      TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, rel_type_id),
    UNIQUE (tenant_id, app_id, code),
    FOREIGN KEY (tenant_id, app_id) REFERENCES iam_apps(tenant_id, app_id) ON DELETE CASCADE
);

CREATE TABLE assets_instances (
    tenant_id   TEXT NOT NULL,
    app_id      TEXT NOT NULL,
    instance_id TEXT NOT NULL,
    model_id    TEXT NOT NULL,
    name        TEXT NOT NULL,
    unique_key  TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'ACTIVE',
    attrs_json  TEXT NOT NULL DEFAULT '{}',
    version     BIGINT NOT NULL DEFAULT 1,
    created_by  TEXT NOT NULL DEFAULT '',
    updated_by  TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, instance_id),
    UNIQUE (tenant_id, app_id, model_id, unique_key),
    FOREIGN KEY (tenant_id, app_id, model_id) REFERENCES assets_models(tenant_id, app_id, model_id)
);
CREATE INDEX idx_assets_instances_model ON assets_instances (tenant_id, app_id, model_id, updated_at DESC);

CREATE TABLE assets_field_uniques (
    tenant_id   TEXT NOT NULL,
    app_id      TEXT NOT NULL,
    model_id    TEXT NOT NULL,
    field_key   TEXT NOT NULL,
    field_value TEXT NOT NULL,
    instance_id TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, model_id, field_key, field_value),
    FOREIGN KEY (tenant_id, app_id, instance_id) REFERENCES assets_instances(tenant_id, app_id, instance_id) ON DELETE CASCADE
);

CREATE TABLE assets_edges (
    tenant_id     TEXT NOT NULL,
    app_id        TEXT NOT NULL,
    edge_id       TEXT NOT NULL,
    tree_kind     TEXT NOT NULL DEFAULT 'custom',
    rel_type_id   TEXT NOT NULL,
    source_id     TEXT NOT NULL,
    target_id     TEXT NOT NULL,
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at    TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, edge_id),
    UNIQUE (tenant_id, app_id, tree_kind, source_id, target_id, rel_type_id),
    CHECK (source_id <> target_id),
    FOREIGN KEY (tenant_id, app_id, source_id) REFERENCES assets_instances(tenant_id, app_id, instance_id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, app_id, target_id) REFERENCES assets_instances(tenant_id, app_id, instance_id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, app_id, rel_type_id) REFERENCES assets_relation_types(tenant_id, app_id, rel_type_id)
);
CREATE UNIQUE INDEX idx_assets_edges_single_parent
    ON assets_edges (tenant_id, app_id, tree_kind, target_id)
    WHERE tree_kind IN ('business', 'resource');

CREATE TABLE assets_delete_operations (
    tenant_id        TEXT NOT NULL,
    app_id           TEXT NOT NULL,
    op_id            TEXT NOT NULL,
    root_instance_id TEXT NOT NULL,
    state            TEXT NOT NULL DEFAULT 'RUNNING',
    total_items      INTEGER NOT NULL DEFAULT 0,
    deleted_items    INTEGER NOT NULL DEFAULT 0,
    error_message    TEXT NOT NULL DEFAULT '',
    created_by       TEXT NOT NULL DEFAULT '',
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, op_id),
    FOREIGN KEY (tenant_id, app_id) REFERENCES iam_apps(tenant_id, app_id) ON DELETE CASCADE
);

CREATE TABLE assets_delete_items (
    tenant_id   TEXT NOT NULL,
    app_id      TEXT NOT NULL,
    op_id       TEXT NOT NULL,
    instance_id TEXT NOT NULL,
    depth       INTEGER NOT NULL DEFAULT 0,
    status      TEXT NOT NULL DEFAULT 'PENDING',
    PRIMARY KEY (tenant_id, app_id, op_id, instance_id),
    FOREIGN KEY (tenant_id, app_id, op_id) REFERENCES assets_delete_operations(tenant_id, app_id, op_id) ON DELETE CASCADE
);

CREATE TABLE cfg_configurations (
    tenant_id    TEXT NOT NULL,
    app_id       TEXT NOT NULL,
    cfg_id       TEXT NOT NULL,
    project_id   TEXT NOT NULL,
    name         TEXT NOT NULL,
    format       TEXT NOT NULL DEFAULT 'kv',
    description  TEXT NOT NULL DEFAULT '',
    entries_json TEXT NOT NULL DEFAULT '[]',
    version      BIGINT NOT NULL DEFAULT 1,
    deleted      INTEGER NOT NULL DEFAULT 0,
    created_by   TEXT NOT NULL DEFAULT '',
    updated_by   TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, cfg_id),
    UNIQUE (tenant_id, app_id, project_id, name),
    FOREIGN KEY (tenant_id, app_id) REFERENCES iam_apps(tenant_id, app_id) ON DELETE CASCADE
);

CREATE TABLE cfg_history (
    tenant_id      TEXT NOT NULL,
    app_id         TEXT NOT NULL,
    cfg_id         TEXT NOT NULL,
    version        BIGINT NOT NULL,
    entries_json   TEXT NOT NULL,
    change_summary TEXT NOT NULL DEFAULT '',
    changed_by     TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, cfg_id, version),
    FOREIGN KEY (tenant_id, app_id, cfg_id) REFERENCES cfg_configurations(tenant_id, app_id, cfg_id) ON DELETE CASCADE
);

CREATE TABLE cfg_environments (
    tenant_id   TEXT NOT NULL,
    app_id      TEXT NOT NULL,
    env_id      TEXT NOT NULL,
    project_id  TEXT NOT NULL,
    code        TEXT NOT NULL,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    version     BIGINT NOT NULL DEFAULT 1,
    created_by  TEXT NOT NULL DEFAULT '',
    updated_by  TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, env_id),
    UNIQUE (tenant_id, app_id, project_id, code),
    FOREIGN KEY (tenant_id, app_id) REFERENCES iam_apps(tenant_id, app_id) ON DELETE CASCADE
);

CREATE TABLE cfg_env_members (
    tenant_id TEXT NOT NULL,
    app_id    TEXT NOT NULL,
    env_id    TEXT NOT NULL,
    cfg_id    TEXT NOT NULL,
    priority  INTEGER NOT NULL,
    PRIMARY KEY (tenant_id, app_id, env_id, cfg_id),
    UNIQUE (tenant_id, app_id, env_id, priority),
    FOREIGN KEY (tenant_id, app_id, env_id) REFERENCES cfg_environments(tenant_id, app_id, env_id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, app_id, cfg_id) REFERENCES cfg_configurations(tenant_id, app_id, cfg_id)
);

CREATE TABLE cfg_env_history (
    tenant_id    TEXT NOT NULL,
    app_id       TEXT NOT NULL,
    env_id       TEXT NOT NULL,
    version      BIGINT NOT NULL,
    members_json TEXT NOT NULL,
    changed_by   TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, env_id, version),
    FOREIGN KEY (tenant_id, app_id, env_id) REFERENCES cfg_environments(tenant_id, app_id, env_id) ON DELETE CASCADE
);

CREATE TABLE deploy_commands (
    tenant_id     TEXT NOT NULL,
    app_id        TEXT NOT NULL,
    principal_id  TEXT NOT NULL,
    operation     TEXT NOT NULL,
    idem_key      TEXT NOT NULL,
    request_hash  TEXT NOT NULL,
    job_id        TEXT NOT NULL,
    http_status   INTEGER NOT NULL,
    response_json TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, principal_id, operation, idem_key),
    FOREIGN KEY (tenant_id, app_id) REFERENCES iam_apps(tenant_id, app_id) ON DELETE CASCADE
);

CREATE TABLE deploy_jobs (
    tenant_id        TEXT NOT NULL,
    app_id           TEXT NOT NULL,
    job_id           TEXT NOT NULL,
    project_id       TEXT NOT NULL DEFAULT '',
    name             TEXT NOT NULL,
    trigger_type     TEXT NOT NULL DEFAULT 'MANUAL',
    state            TEXT NOT NULL DEFAULT 'PENDING',
    cancel_requested INTEGER NOT NULL DEFAULT 0,
    version          BIGINT NOT NULL DEFAULT 1,
    last_seq         BIGINT NOT NULL DEFAULT 0,
    min_retained_seq BIGINT NOT NULL DEFAULT 1,
    spec_json        TEXT NOT NULL DEFAULT '{}',
    summary_json     TEXT NOT NULL DEFAULT '{}',
    created_by       TEXT NOT NULL DEFAULT '',
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    finished_at      TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id, app_id, job_id),
    FOREIGN KEY (tenant_id, app_id) REFERENCES iam_apps(tenant_id, app_id) ON DELETE CASCADE
);
CREATE INDEX idx_deploy_jobs_scope ON deploy_jobs (tenant_id, app_id, created_at DESC);

CREATE TABLE deploy_tasks (
    tenant_id    TEXT NOT NULL,
    app_id       TEXT NOT NULL,
    job_id       TEXT NOT NULL,
    task_id      TEXT NOT NULL,
    task_key     TEXT NOT NULL,
    name         TEXT NOT NULL,
    task_type    TEXT NOT NULL DEFAULT 'CHECK',
    node_id      TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL DEFAULT '{}',
    state        TEXT NOT NULL DEFAULT 'PENDING',
    remaining    INTEGER NOT NULL DEFAULT 0,
    lease_epoch  BIGINT NOT NULL DEFAULT 0,
    attempt_no   INTEGER NOT NULL DEFAULT 0,
    owner        TEXT NOT NULL DEFAULT '',
    lease_until  TEXT NOT NULL DEFAULT '',
    action_key   TEXT NOT NULL DEFAULT '',
    max_retries  INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT NOT NULL DEFAULT '',
    result_json  TEXT NOT NULL DEFAULT '{}',
    started_at   TEXT NOT NULL DEFAULT '',
    finished_at  TEXT NOT NULL DEFAULT '',
    updated_at   TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, job_id, task_id),
    UNIQUE (tenant_id, app_id, job_id, task_key),
    FOREIGN KEY (tenant_id, app_id, job_id) REFERENCES deploy_jobs(tenant_id, app_id, job_id) ON DELETE CASCADE
);
CREATE INDEX idx_deploy_tasks_ready ON deploy_tasks (state, updated_at);
CREATE INDEX idx_deploy_tasks_lease ON deploy_tasks (state, lease_until);

CREATE TABLE deploy_edges (
    tenant_id    TEXT NOT NULL,
    app_id       TEXT NOT NULL,
    job_id       TEXT NOT NULL,
    from_task_id TEXT NOT NULL,
    to_task_id   TEXT NOT NULL,
    satisfied    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, app_id, job_id, from_task_id, to_task_id),
    FOREIGN KEY (tenant_id, app_id, job_id, from_task_id) REFERENCES deploy_tasks(tenant_id, app_id, job_id, task_id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, app_id, job_id, to_task_id) REFERENCES deploy_tasks(tenant_id, app_id, job_id, task_id) ON DELETE CASCADE
);

CREATE TABLE deploy_attempts (
    tenant_id     TEXT NOT NULL,
    app_id        TEXT NOT NULL,
    job_id        TEXT NOT NULL,
    task_id       TEXT NOT NULL,
    attempt_no    INTEGER NOT NULL,
    lease_epoch   BIGINT NOT NULL,
    owner         TEXT NOT NULL,
    action_key    TEXT NOT NULL,
    state         TEXT NOT NULL DEFAULT 'RUNNING',
    error_message TEXT NOT NULL DEFAULT '',
    started_at    TEXT NOT NULL,
    finished_at   TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id, app_id, job_id, task_id, attempt_no),
    UNIQUE (tenant_id, app_id, action_key),
    FOREIGN KEY (tenant_id, app_id, job_id, task_id) REFERENCES deploy_tasks(tenant_id, app_id, job_id, task_id) ON DELETE CASCADE
);

CREATE TABLE deploy_receipts (
    tenant_id    TEXT NOT NULL,
    app_id       TEXT NOT NULL,
    provider     TEXT NOT NULL,
    receipt_id   TEXT NOT NULL,
    job_id       TEXT NOT NULL,
    task_id      TEXT NOT NULL,
    attempt_no   INTEGER NOT NULL,
    lease_epoch  BIGINT NOT NULL,
    status       TEXT NOT NULL,
    digest       TEXT NOT NULL,
    payload_json TEXT NOT NULL DEFAULT '{}',
    received_at  TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, provider, receipt_id),
    FOREIGN KEY (tenant_id, app_id, job_id, task_id) REFERENCES deploy_tasks(tenant_id, app_id, job_id, task_id) ON DELETE CASCADE
);

CREATE TABLE deploy_timeline (
    tenant_id    TEXT NOT NULL,
    app_id       TEXT NOT NULL,
    job_id       TEXT NOT NULL,
    seq          BIGINT NOT NULL,
    event_type   TEXT NOT NULL,
    task_id      TEXT NOT NULL DEFAULT '',
    attempt_no   INTEGER NOT NULL DEFAULT 0,
    lease_epoch  BIGINT NOT NULL DEFAULT 0,
    message      TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL DEFAULT '{}',
    created_at   TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, job_id, seq),
    FOREIGN KEY (tenant_id, app_id, job_id) REFERENCES deploy_jobs(tenant_id, app_id, job_id) ON DELETE CASCADE
);

CREATE TABLE sys_outbox_events (
    event_id       TEXT PRIMARY KEY,
    tenant_id      TEXT NOT NULL,
    app_id         TEXT NOT NULL,
    dedup_key      TEXT NOT NULL,
    topic          TEXT NOT NULL,
    aggregate_type TEXT NOT NULL,
    aggregate_id   TEXT NOT NULL,
    payload_json   TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'PENDING',
    published_at   TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL,
    UNIQUE (tenant_id, app_id, dedup_key)
);
CREATE INDEX idx_sys_outbox_pending ON sys_outbox_events (status, created_at);

CREATE TABLE sys_inbox_events (
    tenant_id    TEXT NOT NULL,
    app_id       TEXT NOT NULL,
    consumer     TEXT NOT NULL,
    message_id   TEXT NOT NULL,
    digest       TEXT NOT NULL,
    processed_at TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, consumer, message_id)
);

CREATE TABLE nodes_enroll_tokens (
    tenant_id       TEXT NOT NULL,
    app_id          TEXT NOT NULL,
    token_id        TEXT NOT NULL,
    token_hash      TEXT NOT NULL UNIQUE,
    labels_json     TEXT NOT NULL DEFAULT '{}',
    expires_at      TEXT NOT NULL,
    used_at         TEXT NOT NULL DEFAULT '',
    used_by_node_id TEXT NOT NULL DEFAULT '',
    created_by      TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, token_id),
    FOREIGN KEY (tenant_id, app_id) REFERENCES iam_apps(tenant_id, app_id) ON DELETE CASCADE
);

CREATE TABLE nodes_registry (
    tenant_id       TEXT NOT NULL,
    app_id          TEXT NOT NULL,
    node_id         TEXT NOT NULL,
    hostname        TEXT NOT NULL,
    ip_address      TEXT NOT NULL DEFAULT '',
    os_arch         TEXT NOT NULL DEFAULT 'linux/amd64',
    agent_version   TEXT NOT NULL DEFAULT '1.0.0',
    status          TEXT NOT NULL DEFAULT 'ONLINE',
    labels_json     TEXT NOT NULL DEFAULT '{}',
    token_hash      TEXT NOT NULL,
    prev_token_hash TEXT NOT NULL DEFAULT '',
    prev_expires_at TEXT NOT NULL DEFAULT '',
    cred_version    BIGINT NOT NULL DEFAULT 1,
    last_seen_at    TEXT NOT NULL,
    enrolled_at     TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, node_id),
    FOREIGN KEY (tenant_id, app_id) REFERENCES iam_apps(tenant_id, app_id) ON DELETE CASCADE
);

CREATE TABLE nodes_host_groups (
    tenant_id   TEXT NOT NULL,
    app_id      TEXT NOT NULL,
    group_id    TEXT NOT NULL,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, group_id),
    UNIQUE (tenant_id, app_id, name),
    FOREIGN KEY (tenant_id, app_id) REFERENCES iam_apps(tenant_id, app_id) ON DELETE CASCADE
);

CREATE TABLE nodes_group_members (
    tenant_id  TEXT NOT NULL,
    app_id     TEXT NOT NULL,
    group_id   TEXT NOT NULL,
    node_id    TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, group_id, node_id),
    FOREIGN KEY (tenant_id, app_id, group_id) REFERENCES nodes_host_groups(tenant_id, app_id, group_id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, app_id, node_id) REFERENCES nodes_registry(tenant_id, app_id, node_id) ON DELETE CASCADE
);

CREATE TABLE nodes_distributions (
    tenant_id        TEXT NOT NULL,
    app_id           TEXT NOT NULL,
    dist_id          TEXT NOT NULL,
    artifact_file_id TEXT NOT NULL DEFAULT '',
    target_node_id   TEXT NOT NULL,
    total_bytes      BIGINT NOT NULL,
    verified_bytes   BIGINT NOT NULL DEFAULT 0,
    sha256           TEXT NOT NULL,
    state            TEXT NOT NULL DEFAULT 'PENDING',
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, dist_id),
    FOREIGN KEY (tenant_id, app_id, target_node_id) REFERENCES nodes_registry(tenant_id, app_id, node_id)
);

CREATE TABLE nodes_distribution_checkpoints (
    tenant_id    TEXT NOT NULL,
    app_id       TEXT NOT NULL,
    dist_id      TEXT NOT NULL,
    chunk_index  INTEGER NOT NULL,
    chunk_offset BIGINT NOT NULL,
    chunk_bytes  BIGINT NOT NULL,
    chunk_sha256 TEXT NOT NULL,
    acked_at     TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, dist_id, chunk_index),
    FOREIGN KEY (tenant_id, app_id, dist_id) REFERENCES nodes_distributions(tenant_id, app_id, dist_id) ON DELETE CASCADE
);

CREATE TABLE gw_revisions (
    tenant_id     TEXT NOT NULL,
    app_id        TEXT NOT NULL,
    route_version BIGINT NOT NULL DEFAULT 1,
    updated_at    TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id),
    FOREIGN KEY (tenant_id, app_id) REFERENCES iam_apps(tenant_id, app_id) ON DELETE CASCADE
);

CREATE TABLE gw_routers (
    tenant_id      TEXT NOT NULL,
    app_id         TEXT NOT NULL,
    router_id      TEXT NOT NULL,
    name           TEXT NOT NULL,
    listen_host    TEXT NOT NULL DEFAULT '*',
    rate_limit_qps INTEGER NOT NULL DEFAULT 1000,
    burst          INTEGER NOT NULL DEFAULT 200,
    status         TEXT NOT NULL DEFAULT 'ACTIVE',
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, router_id),
    UNIQUE (tenant_id, app_id, name),
    FOREIGN KEY (tenant_id, app_id) REFERENCES iam_apps(tenant_id, app_id) ON DELETE CASCADE
);

CREATE TABLE gw_routes (
    tenant_id      TEXT NOT NULL,
    app_id         TEXT NOT NULL,
    route_id       TEXT NOT NULL,
    router_id      TEXT NOT NULL,
    name           TEXT NOT NULL,
    path_prefix    TEXT NOT NULL,
    upstream_url   TEXT NOT NULL,
    is_default     INTEGER NOT NULL DEFAULT 0,
    strip_prefix   INTEGER NOT NULL DEFAULT 0,
    timeout_ms     INTEGER NOT NULL DEFAULT 30000,
    rate_limit_qps INTEGER NOT NULL DEFAULT 0,
    status         TEXT NOT NULL DEFAULT 'ACTIVE',
    version        BIGINT NOT NULL DEFAULT 1,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, route_id),
    FOREIGN KEY (tenant_id, app_id, router_id) REFERENCES gw_routers(tenant_id, app_id, router_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX idx_gw_routes_single_default
    ON gw_routes (tenant_id, app_id, router_id)
    WHERE is_default = 1;

CREATE TABLE gw_telemetry_samples (
    tenant_id        TEXT NOT NULL,
    app_id           TEXT NOT NULL,
    router_id        TEXT NOT NULL,
    bucket_kind      TEXT NOT NULL,
    bucket_key       TEXT NOT NULL,
    total_requests   BIGINT NOT NULL DEFAULT 0,
    error_requests   BIGINT NOT NULL DEFAULT 0,
    total_latency_ms BIGINT NOT NULL DEFAULT 0,
    updated_at       TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, router_id, bucket_kind, bucket_key),
    FOREIGN KEY (tenant_id, app_id, router_id) REFERENCES gw_routers(tenant_id, app_id, router_id) ON DELETE CASCADE
);

CREATE TABLE artifact_files (
    tenant_id           TEXT NOT NULL,
    app_id              TEXT NOT NULL,
    file_id             TEXT NOT NULL,
    filename            TEXT NOT NULL,
    content_type        TEXT NOT NULL DEFAULT 'application/octet-stream',
    size_bytes          BIGINT NOT NULL DEFAULT 0,
    sha256              TEXT NOT NULL,
    storage_path        TEXT NOT NULL,
    verification_status TEXT NOT NULL DEFAULT 'PENDING',
    verified_at         TEXT NOT NULL DEFAULT '',
    created_by          TEXT NOT NULL DEFAULT '',
    created_at          TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, file_id),
    FOREIGN KEY (tenant_id, app_id) REFERENCES iam_apps(tenant_id, app_id) ON DELETE CASCADE
);

CREATE TABLE artifact_releases (
    tenant_id   TEXT NOT NULL,
    app_id      TEXT NOT NULL,
    release_id  TEXT NOT NULL,
    name        TEXT NOT NULL,
    version     TEXT NOT NULL,
    file_id     TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'DRAFT',
    notes       TEXT NOT NULL DEFAULT '',
    promoted_at TEXT NOT NULL DEFAULT '',
    created_by  TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, release_id),
    UNIQUE (tenant_id, app_id, name, version),
    FOREIGN KEY (tenant_id, app_id, file_id) REFERENCES artifact_files(tenant_id, app_id, file_id)
);

CREATE TABLE artifact_attachments (
    tenant_id     TEXT NOT NULL,
    app_id        TEXT NOT NULL,
    attachment_id TEXT NOT NULL,
    owner_type    TEXT NOT NULL,
    owner_id      TEXT NOT NULL,
    file_id       TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    PRIMARY KEY (tenant_id, app_id, attachment_id),
    FOREIGN KEY (tenant_id, app_id, file_id) REFERENCES artifact_files(tenant_id, app_id, file_id)
);

-- ============================================================================
-- PostgreSQL 16 FORCE ROW LEVEL SECURITY (Multi-Tenant Physical Isolation)
-- ============================================================================
-- +goose StatementBegin
DO $$
DECLARE
    tbl TEXT;
BEGIN
    FOREACH tbl IN ARRAY ARRAY[
        'iam_apps','iam_app_keys','iam_projects','iam_project_roles','iam_project_role_perms',
        'iam_scope_versions','iam_audit_logs',
        'assets_model_groups','assets_models','assets_model_fields','assets_relation_types',
        'assets_instances','assets_field_uniques','assets_edges','assets_delete_operations','assets_delete_items',
        'cfg_configurations','cfg_history','cfg_environments','cfg_env_members','cfg_env_history',
        'deploy_commands','deploy_jobs','deploy_tasks','deploy_edges','deploy_attempts','deploy_receipts','deploy_timeline',
        'sys_outbox_events','sys_inbox_events',
        'nodes_enroll_tokens','nodes_registry','nodes_host_groups','nodes_group_members','nodes_distributions','nodes_distribution_checkpoints',
        'gw_revisions','gw_routers','gw_routes','gw_telemetry_samples',
        'artifact_files','artifact_releases','artifact_attachments'
    ] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', tbl);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', tbl);
        EXECUTE format(
            'CREATE POLICY liveops_scope_isolation ON %I USING (
                COALESCE(current_setting(''app.tenant_id'', true), '''') = ''''
                OR (
                    tenant_id = current_setting(''app.tenant_id'', true)
                    AND (
                        COALESCE(current_setting(''app.app_id'', true), '''') = ''''
                        OR app_id = current_setting(''app.app_id'', true)
                    )
                )
            )',
            tbl
        );
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS artifact_attachments;
DROP TABLE IF EXISTS artifact_releases;
DROP TABLE IF EXISTS artifact_files;
DROP TABLE IF EXISTS gw_telemetry_samples;
DROP TABLE IF EXISTS gw_routes;
DROP TABLE IF EXISTS gw_routers;
DROP TABLE IF EXISTS gw_revisions;
DROP TABLE IF EXISTS nodes_distribution_checkpoints;
DROP TABLE IF EXISTS nodes_distributions;
DROP TABLE IF EXISTS nodes_group_members;
DROP TABLE IF EXISTS nodes_host_groups;
DROP TABLE IF EXISTS nodes_registry;
DROP TABLE IF EXISTS nodes_enroll_tokens;
DROP TABLE IF EXISTS sys_inbox_events;
DROP TABLE IF EXISTS sys_outbox_events;
DROP TABLE IF EXISTS deploy_timeline;
DROP TABLE IF EXISTS deploy_receipts;
DROP TABLE IF EXISTS deploy_attempts;
DROP TABLE IF EXISTS deploy_edges;
DROP TABLE IF EXISTS deploy_tasks;
DROP TABLE IF EXISTS deploy_jobs;
DROP TABLE IF EXISTS deploy_commands;
DROP TABLE IF EXISTS cfg_env_history;
DROP TABLE IF EXISTS cfg_env_members;
DROP TABLE IF EXISTS cfg_environments;
DROP TABLE IF EXISTS cfg_history;
DROP TABLE IF EXISTS cfg_configurations;
DROP TABLE IF EXISTS assets_delete_items;
DROP TABLE IF EXISTS assets_delete_operations;
DROP TABLE IF EXISTS assets_edges;
DROP TABLE IF EXISTS assets_field_uniques;
DROP TABLE IF EXISTS assets_instances;
DROP TABLE IF EXISTS assets_relation_types;
DROP TABLE IF EXISTS assets_model_fields;
DROP TABLE IF EXISTS assets_models;
DROP TABLE IF EXISTS assets_model_groups;
DROP TABLE IF EXISTS iam_audit_logs;
DROP TABLE IF EXISTS iam_scope_versions;
DROP TABLE IF EXISTS iam_route_policies;
DROP TABLE IF EXISTS iam_project_role_perms;
DROP TABLE IF EXISTS iam_project_roles;
DROP TABLE IF EXISTS iam_user_roles;
DROP TABLE IF EXISTS iam_role_perms;
DROP TABLE IF EXISTS iam_perms;
DROP TABLE IF EXISTS iam_roles;
DROP TABLE IF EXISTS iam_users;
DROP TABLE IF EXISTS iam_projects;
DROP TABLE IF EXISTS iam_app_keys;
DROP TABLE IF EXISTS iam_apps;
DROP TABLE IF EXISTS iam_tenants;
