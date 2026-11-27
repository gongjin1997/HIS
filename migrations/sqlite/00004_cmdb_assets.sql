-- +goose Up
-- SQLite dialect: Lightweight CMDB Dynamic Groups & External Sync Sources (Tables 25-26)

CREATE TABLE IF NOT EXISTS cmdb_dynamic_groups (
    id          VARCHAR(48) PRIMARY KEY,
    project_id  VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        VARCHAR(128) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    rules_json  TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(rules_json)),
    created_by  VARCHAR(128) NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (project_id, name)
);
CREATE INDEX IF NOT EXISTS idx_cmdb_dgrp_project ON cmdb_dynamic_groups(project_id);

CREATE TABLE IF NOT EXISTS cmdb_sync_sources (
    id            VARCHAR(48) PRIMARY KEY,
    project_id    VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name          VARCHAR(128) NOT NULL,
    kind          VARCHAR(32) NOT NULL CHECK (kind IN ('http_json','http_csv','liveops_json','ansible_ini','huawei_hcs')),
    endpoint      TEXT NOT NULL DEFAULT '',
    credential_id VARCHAR(48),
    mapping_json  TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(mapping_json)),
    payload_text  TEXT NOT NULL DEFAULT '',
    last_sync_at  TEXT,
    last_status   VARCHAR(24) NOT NULL DEFAULT 'never',
    last_summary  TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (project_id, name)
);
CREATE INDEX IF NOT EXISTS idx_cmdb_sync_project ON cmdb_sync_sources(project_id);
