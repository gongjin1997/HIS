-- +goose Up
-- PostgreSQL 18 dialect: Table 14 operation_approvals with DB-enforced anti-self-approval constraint (G1 §4.2)

CREATE TABLE IF NOT EXISTS operation_approvals (
    operation_id  VARCHAR(48) PRIMARY KEY REFERENCES operations(id) ON DELETE CASCADE,
    project_id    VARCHAR(48) NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    env_name      VARCHAR(32) NOT NULL,
    requested_by  VARCHAR(128) NOT NULL,
    reason        TEXT NOT NULL DEFAULT '',
    status        VARCHAR(24) NOT NULL CHECK (status IN ('pending','approved','rejected','expired','cancelled')),
    decided_by    VARCHAR(128),
    decision_note TEXT NOT NULL DEFAULT '',
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_at    TIMESTAMPTZ,
    CONSTRAINT chk_no_self_approval CHECK (decided_by IS NULL OR decided_by <> requested_by)
);
