-- +goose Up
-- PostgreSQL 18 dialect: Lightweight CMDB Dynamic Groups & External Sync Sources (Tables 25-26)

CREATE TABLE IF NOT EXISTS cmdb_dynamic_groups (
    id          VARCHAR(48) PRIMARY KEY,
    project_id  VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        VARCHAR(128) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    rules_json  JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_by  VARCHAR(128) NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
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
    mapping_json  JSONB NOT NULL DEFAULT '{}'::jsonb,
    payload_text  TEXT NOT NULL DEFAULT '',
    last_sync_at  TIMESTAMPTZ,
    last_status   VARCHAR(24) NOT NULL DEFAULT 'never',
    last_summary  TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, name)
);
CREATE INDEX IF NOT EXISTS idx_cmdb_sync_project ON cmdb_sync_sources(project_id);
