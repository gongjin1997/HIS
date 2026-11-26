-- +goose Up
-- SQLite dialect: 10 platform control-plane tables (G1 §6.3)

CREATE TABLE IF NOT EXISTS media_policies (
    id              VARCHAR(48) PRIMARY KEY,
    scope           VARCHAR(16) NOT NULL DEFAULT 'project' CHECK (scope IN ('global', 'project', 'instance')),
    project_id      VARCHAR(48) REFERENCES projects(id) ON DELETE RESTRICT,
    instance_id     VARCHAR(48) REFERENCES instances(id) ON DELETE CASCADE,
    name            VARCHAR(128) NOT NULL,
    is_default      INTEGER NOT NULL DEFAULT 0,
    delivery_mode   VARCHAR(24) NOT NULL DEFAULT 'auto' CHECK (delivery_mode IN ('auto', 'relay-cache', 'direct-presigned')),
    offline_strict  INTEGER NOT NULL DEFAULT 0,
    strict_local    INTEGER NOT NULL DEFAULT 0,
    sources_json    TEXT NOT NULL,
    last_probe_json TEXT NOT NULL DEFAULT '{}',
    last_probed_at  TEXT,
    version         INTEGER NOT NULL DEFAULT 1,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    CHECK (
        (scope = 'global'   AND project_id IS NULL     AND instance_id IS NULL) OR
        (scope = 'project'  AND project_id IS NOT NULL AND instance_id IS NULL) OR
        (scope = 'instance' AND project_id IS NOT NULL AND instance_id IS NOT NULL)
    ),
    UNIQUE (scope, project_id, instance_id, name)
);

CREATE TABLE IF NOT EXISTS users (
    id                    VARCHAR(48) PRIMARY KEY,
    username              VARCHAR(128) NOT NULL UNIQUE,
    display_name          VARCHAR(128) NOT NULL DEFAULT '',
    auth_source           VARCHAR(16) NOT NULL CHECK (auth_source IN ('local', 'oidc')),
    oidc_issuer           VARCHAR(255) NOT NULL DEFAULT '',
    oidc_subject          VARCHAR(255) NOT NULL DEFAULT '',
    password_hash         TEXT NOT NULL DEFAULT '',
    role_bindings_json    TEXT NOT NULL DEFAULT '[]',
    failed_login_attempts INTEGER NOT NULL DEFAULT 0,
    locked_until          TEXT,
    disabled              INTEGER NOT NULL DEFAULT 0,
    created_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_oidc_sub ON users(oidc_issuer, oidc_subject) WHERE auth_source = 'oidc';

CREATE TABLE IF NOT EXISTS user_sessions (
    id          VARCHAR(48) PRIMARY KEY,
    user_id     VARCHAR(48) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  CHAR(64) NOT NULL UNIQUE,
    source_ip   VARCHAR(64) NOT NULL DEFAULT '',
    user_agent  VARCHAR(255) NOT NULL DEFAULT '',
    expires_at  TEXT NOT NULL,
    revoked_at  TEXT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX IF NOT EXISTS idx_user_sessions_exp ON user_sessions(expires_at);

CREATE TABLE IF NOT EXISTS api_tokens (
    id           VARCHAR(48) PRIMARY KEY,
    user_id      VARCHAR(48) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id   VARCHAR(48) REFERENCES projects(id) ON DELETE CASCADE,
    name         VARCHAR(128) NOT NULL,
    token_prefix VARCHAR(16) NOT NULL,
    token_hash   CHAR(64) NOT NULL UNIQUE,
    scopes_json  TEXT NOT NULL DEFAULT '[]',
    expires_at   TEXT NOT NULL,
    last_used_at TEXT,
    revoked_at   TEXT,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS idempotency_keys (
    project_id    VARCHAR(48) NOT NULL,
    actor_id      VARCHAR(128) NOT NULL,
    key_hash      CHAR(64) NOT NULL,
    endpoint      VARCHAR(128) NOT NULL,
    request_hash  CHAR(64) NOT NULL,
    status        VARCHAR(16) NOT NULL CHECK (status IN ('in_progress', 'completed')),
    response_code INTEGER,
    response_body TEXT,
    expires_at    TEXT NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    PRIMARY KEY (project_id, actor_id, key_hash)
);
CREATE INDEX IF NOT EXISTS idx_idem_expires ON idempotency_keys(expires_at);

CREATE TABLE IF NOT EXISTS plans (
    id                VARCHAR(48) PRIMARY KEY,
    project_id        VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    target_type       VARCHAR(24) NOT NULL CHECK (target_type IN ('host', 'instance', 'stack')),
    target_id         VARCHAR(48) NOT NULL,
    input_hash        CHAR(64) NOT NULL,
    diff_json         TEXT NOT NULL,
    warnings_json     TEXT NOT NULL DEFAULT '[]',
    pipeline_json     TEXT NOT NULL,
    created_by        VARCHAR(128) NOT NULL,
    expires_at        TEXT NOT NULL,
    consumed_by_op_id VARCHAR(48) REFERENCES operations(id) ON DELETE SET NULL,
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX IF NOT EXISTS idx_plans_target ON plans(target_type, target_id, created_at DESC);

CREATE TABLE IF NOT EXISTS instance_state_backups (
    id                VARCHAR(48) PRIMARY KEY,
    instance_id       VARCHAR(48) NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    from_schema_ver   INTEGER NOT NULL,
    to_schema_ver     INTEGER NOT NULL,
    raw_state_json    TEXT NOT NULL,
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS notification_channels (
    id             VARCHAR(48) PRIMARY KEY,
    project_id     VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name           VARCHAR(128) NOT NULL,
    kind           VARCHAR(24) NOT NULL CHECK (kind IN ('webhook', 'email', 'wecom', 'dingtalk', 'feishu')),
    config_json    TEXT NOT NULL,
    credential_ref VARCHAR(48) REFERENCES credentials(id) ON DELETE RESTRICT,
    events_json    TEXT NOT NULL DEFAULT '[]',
    enabled        INTEGER NOT NULL DEFAULT 1,
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (project_id, name)
);

CREATE TABLE IF NOT EXISTS notification_deliveries (
    id            VARCHAR(48) PRIMARY KEY,
    channel_id    VARCHAR(48) NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
    dedup_key     VARCHAR(128) NOT NULL,
    event_count   INTEGER NOT NULL DEFAULT 1,
    status        VARCHAR(16) NOT NULL CHECK (status IN ('pending', 'sent', 'failed')),
    attempt       INTEGER NOT NULL DEFAULT 0,
    last_error    TEXT NOT NULL DEFAULT '',
    window_end_at TEXT NOT NULL,
    sent_at       TEXT,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX IF NOT EXISTS idx_notify_dedup ON notification_deliveries(channel_id, dedup_key, window_end_at);

CREATE TABLE IF NOT EXISTS cluster_kubeconfigs (
    id              VARCHAR(48) PRIMARY KEY,
    instance_id     VARCHAR(48) NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    user_id         VARCHAR(48) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    k8s_role        VARCHAR(64) NOT NULL CHECK (k8s_role IN ('cluster-admin', 'ns-admin', 'edit', 'view')),
    namespaces_json TEXT NOT NULL DEFAULT '[]',
    cert_serial     VARCHAR(128) NOT NULL DEFAULT '',
    expires_at      TEXT NOT NULL,
    revoked_at      TEXT,
    revoked_by      VARCHAR(128),
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX IF NOT EXISTS idx_cluster_kc_inst ON cluster_kubeconfigs(instance_id, expires_at);
