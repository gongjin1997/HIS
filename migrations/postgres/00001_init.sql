-- +goose Up
-- PostgreSQL 18 dialect: 13 core base tables (D2 §1.3)

CREATE TABLE projects (
    id          VARCHAR(48) PRIMARY KEY,
    name        VARCHAR(64) NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE credentials (
    id            VARCHAR(48) PRIMARY KEY,
    project_id    VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    name          VARCHAR(128) NOT NULL,
    kind          VARCHAR(32) NOT NULL,
    state         VARCHAR(24) NOT NULL,
    key_version   INTEGER NOT NULL DEFAULT 1,
    envelope_json JSONB NOT NULL,
    prev_envelope JSONB,
    created_by    VARCHAR(128) NOT NULL,
    version       BIGINT NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, name)
);

CREATE TABLE hosts (
    id              VARCHAR(48) PRIMARY KEY,
    project_id      VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    name            VARCHAR(128) NOT NULL,
    address         VARCHAR(128) NOT NULL,
    ssh_port        INTEGER NOT NULL DEFAULT 22,
    ssh_user        VARCHAR(64) NOT NULL,
    credential_id   VARCHAR(48) NOT NULL REFERENCES credentials(id) ON DELETE RESTRICT,
    jump_host_id    VARCHAR(48) REFERENCES hosts(id) ON DELETE RESTRICT,
    legacy_rsa      BOOLEAN NOT NULL DEFAULT FALSE,
    work_dir        VARCHAR(255) NOT NULL DEFAULT '/var/lib/kinstall/tmp',
    hostkey_mode    VARCHAR(16) NOT NULL DEFAULT 'tofu',
    hostkey_fp      VARCHAR(128) NOT NULL DEFAULT '',
    state           VARCHAR(24) NOT NULL DEFAULT 'pending',
    fault_zone      VARCHAR(64) NOT NULL DEFAULT '',
    fault_rack      VARCHAR(64) NOT NULL DEFAULT '',
    labels_json     JSONB NOT NULL DEFAULT '{}',
    facts_json      JSONB NOT NULL DEFAULT '{}',
    facts_at        TIMESTAMPTZ,
    os_family       VARCHAR(32) NOT NULL DEFAULT '',
    os_id           VARCHAR(64) NOT NULL DEFAULT '',
    os_version      VARCHAR(32) NOT NULL DEFAULT '',
    arch            VARCHAR(16) NOT NULL DEFAULT '',
    page_size       INTEGER NOT NULL DEFAULT 4096,
    init_profile    VARCHAR(64) NOT NULL DEFAULT '',
    version         BIGINT NOT NULL DEFAULT 1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, name),
    UNIQUE (project_id, address, ssh_port)
);
CREATE INDEX idx_hosts_project_state ON hosts(project_id, state);
CREATE INDEX idx_hosts_fault_domain ON hosts(project_id, fault_zone, fault_rack);

CREATE TABLE stacks (
    id                VARCHAR(48) PRIMARY KEY,
    project_id        VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    env               VARCHAR(32) NOT NULL,
    name              VARCHAR(128) NOT NULL,
    spec_yaml         TEXT NOT NULL,
    resolved_dag_json JSONB NOT NULL DEFAULT '{}',
    state             VARCHAR(24) NOT NULL DEFAULT 'pending',
    version           BIGINT NOT NULL DEFAULT 1,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, env, name)
);

CREATE TABLE instances (
    id                  VARCHAR(48) PRIMARY KEY,
    project_id          VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    stack_id            VARCHAR(48) REFERENCES stacks(id) ON DELETE RESTRICT,
    env                 VARCHAR(32) NOT NULL,
    name                VARCHAR(128) NOT NULL,
    product             VARCHAR(64) NOT NULL,
    deploy_target       VARCHAR(24) NOT NULL,
    product_version     VARCHAR(64) NOT NULL,
    bom_revision        VARCHAR(64) NOT NULL,
    plugin_schema_ver   INTEGER NOT NULL DEFAULT 1,
    desired_spec_json   JSONB NOT NULL,
    last_applied_json   JSONB,
    state_json          JSONB NOT NULL DEFAULT '{}',
    outputs_json        JSONB NOT NULL DEFAULT '{}',
    state               VARCHAR(24) NOT NULL DEFAULT 'pending',
    version             BIGINT NOT NULL DEFAULT 1,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, env, name)
);
CREATE INDEX idx_instances_stack ON instances(stack_id);

CREATE TABLE instance_host_claims (
    id           VARCHAR(48) PRIMARY KEY,
    instance_id  VARCHAR(48) NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    host_id      VARCHAR(48) NOT NULL REFERENCES hosts(id) ON DELETE RESTRICT,
    role         VARCHAR(64) NOT NULL,
    ports_json   JSONB NOT NULL DEFAULT '[]',
    paths_json   JSONB NOT NULL DEFAULT '[]',
    subnets_json JSONB NOT NULL DEFAULT '[]',
    mem_mb       INTEGER NOT NULL DEFAULT 0,
    UNIQUE (instance_id, host_id, role)
);
CREATE INDEX idx_claims_host ON instance_host_claims(host_id);

CREATE TABLE operations (
    id               VARCHAR(48) PRIMARY KEY,
    project_id       VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    target_type      VARCHAR(24) NOT NULL,
    target_id        VARCHAR(48) NOT NULL,
    action           VARCHAR(32) NOT NULL,
    state            VARCHAR(24) NOT NULL CHECK (state IN (
                         'awaiting_approval', 'pending', 'running',
                         'succeeded', 'failed', 'rejected', 'cancelled'
                     )),
    idempotency_key  VARCHAR(128),
    requested_by     VARCHAR(128) NOT NULL,
    approved_by      VARCHAR(128),
    pipeline_json    JSONB NOT NULL,
    lease_owner      VARCHAR(128),
    lease_expires_at TIMESTAMPTZ,
    fencing_token    BIGINT NOT NULL DEFAULT 0,
    error_code       VARCHAR(32),
    error_message    TEXT,
    started_at       TIMESTAMPTZ,
    finished_at      TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, idempotency_key)
);
CREATE INDEX idx_ops_claimable ON operations(state, lease_expires_at) WHERE state IN ('pending', 'running');
CREATE INDEX idx_ops_target ON operations(target_type, target_id, created_at DESC);

CREATE TABLE task_runs (
    id                  VARCHAR(48) PRIMARY KEY,
    operation_id        VARCHAR(48) NOT NULL REFERENCES operations(id) ON DELETE CASCADE,
    phase_idx           INTEGER NOT NULL,
    step_key            VARCHAR(128) NOT NULL,
    host_id             VARCHAR(48) REFERENCES hosts(id) ON DELETE SET NULL,
    task_type           VARCHAR(64) NOT NULL,
    params_digest       CHAR(64) NOT NULL,
    state               VARCHAR(24) NOT NULL,
    attempt             INTEGER NOT NULL DEFAULT 0,
    fencing_token       BIGINT NOT NULL DEFAULT 0,
    remote_run_dir      VARCHAR(255),
    remote_pgid         INTEGER,
    remote_log_offset   BIGINT NOT NULL DEFAULT 0,
    drift_summary_json  JSONB,
    backup_ref_json     JSONB,
    error_code          VARCHAR(32),
    error_detail        TEXT,
    started_at          TIMESTAMPTZ,
    finished_at         TIMESTAMPTZ,
    UNIQUE (operation_id, phase_idx, step_key)
);
CREATE INDEX idx_task_runs_op ON task_runs(operation_id, phase_idx);

CREATE TABLE operation_events (
    seq           BIGSERIAL PRIMARY KEY,
    operation_id  VARCHAR(48) NOT NULL REFERENCES operations(id) ON DELETE CASCADE,
    task_run_id   VARCHAR(48),
    event_type    VARCHAR(32) NOT NULL,
    payload_json  JSONB NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_op_events_replay ON operation_events(operation_id, seq);

CREATE TABLE resource_locks (
    resource_type  VARCHAR(16) NOT NULL,
    resource_id    VARCHAR(48) NOT NULL,
    lock_mode      VARCHAR(16) NOT NULL,
    holder_op_id   VARCHAR(48) NOT NULL REFERENCES operations(id) ON DELETE CASCADE,
    fencing_token  BIGINT NOT NULL,
    expires_at     TIMESTAMPTZ NOT NULL,
    acquired_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (resource_type, resource_id, holder_op_id)
);
CREATE INDEX idx_locks_lookup ON resource_locks(resource_type, resource_id, expires_at);

CREATE TABLE audit_chain_head (
    chain_id    SMALLINT PRIMARY KEY CHECK (chain_id = 1),
    last_seq    BIGINT NOT NULL DEFAULT 0,
    last_sha256 CHAR(64) NOT NULL DEFAULT '0000000000000000000000000000000000000000000000000000000000000000',
    last_sm3    CHAR(64) NOT NULL DEFAULT '0000000000000000000000000000000000000000000000000000000000000000'
);
INSERT INTO audit_chain_head (chain_id) VALUES (1) ON CONFLICT DO NOTHING;

CREATE TABLE audit_logs (
    seq            BIGINT PRIMARY KEY,
    occurred_at    TIMESTAMPTZ NOT NULL,
    actor_id       VARCHAR(128) NOT NULL,
    actor_type     VARCHAR(24) NOT NULL,
    source_ip      VARCHAR(64) NOT NULL DEFAULT '',
    project_id     VARCHAR(48) NOT NULL DEFAULT '',
    action         VARCHAR(64) NOT NULL,
    target_type    VARCHAR(32) NOT NULL,
    target_id      VARCHAR(64) NOT NULL,
    result         VARCHAR(16) NOT NULL,
    err_code       VARCHAR(32) NOT NULL DEFAULT '',
    trace_id       VARCHAR(64) NOT NULL DEFAULT '',
    payload_json   TEXT NOT NULL,
    prev_sha256    CHAR(64) NOT NULL,
    record_sha256  CHAR(64) NOT NULL,
    prev_sm3       VARCHAR(64) NOT NULL DEFAULT '',
    record_sm3     VARCHAR(64) NOT NULL DEFAULT ''
);
CREATE INDEX idx_audit_logs_time ON audit_logs(occurred_at DESC);
CREATE INDEX idx_audit_logs_target ON audit_logs(project_id, target_type, target_id, seq DESC);

CREATE TABLE audit_syslog_cursor (
    sink_id        VARCHAR(64) PRIMARY KEY,
    last_sent_seq  BIGINT NOT NULL DEFAULT 0,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
