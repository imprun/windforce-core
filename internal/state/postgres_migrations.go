package state

import (
	"context"
)

const postgresMigrationAdvisoryLockID int64 = 0x57464c4d49475241

func (s *PostgresStore) Migrate(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, postgresMigrationAdvisoryLockID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
CREATE TABLE IF NOT EXISTS runs (
    id TEXT PRIMARY KEY,
    adapter TEXT NOT NULL,
    app TEXT NOT NULL,
    action TEXT NOT NULL,
    state TEXT NOT NULL,
    deployment JSONB NOT NULL,
    input JSONB NOT NULL,
    output JSONB,
    result JSONB,
    error JSONB,
    task_id TEXT,
    correlation_id TEXT,
    env JSONB,
    client_id TEXT,
    principal_kind TEXT,
    principal_id TEXT,
    idempotency_hash TEXT,
    request_fingerprint TEXT,
    created_by TEXT NOT NULL DEFAULT 'system',
    permissioned_as TEXT NOT NULL DEFAULT 'system',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS schema_migration (
    name TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id),
    state TEXT NOT NULL,
    kind TEXT NOT NULL,
    payload JSONB NOT NULL,
    priority INTEGER NOT NULL DEFAULT 100,
    attempt INTEGER NOT NULL DEFAULT 0,
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    canceled_by TEXT,
    canceled_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS human_tasks (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id),
    state TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT,
    schema JSONB,
    resume_input JSONB,
    token_hash TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS run_events (
    id BIGSERIAL PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES runs(id),
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS job_logs (
    job_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL DEFAULT 'default',
    logs TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS job_log_state (
    job_id TEXT PRIMARY KEY REFERENCES jobs(id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL DEFAULT 'default',
    next_offset BIGINT NOT NULL DEFAULT 0 CHECK (next_offset >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS job_log_chunks (
    id BIGSERIAL PRIMARY KEY,
    job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL DEFAULT 'default',
    start_offset BIGINT NOT NULL CHECK (start_offset >= 0),
    chunk TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (job_id, start_offset)
);

CREATE INDEX IF NOT EXISTS job_log_chunks_job_cursor_idx
ON job_log_chunks (workspace_id, job_id, start_offset);

CREATE TABLE IF NOT EXISTS job_state (
    workspace_id TEXT NOT NULL,
    state_path TEXT NOT NULL,
    value JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, state_path)
);

CREATE TABLE IF NOT EXISTS variable (
    workspace_id TEXT NOT NULL,
    app_key TEXT NOT NULL DEFAULT '',
    path TEXT NOT NULL,
    value TEXT NOT NULL,
    is_secret BOOLEAN NOT NULL DEFAULT false,
    description TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (workspace_id, app_key, path)
);

CREATE TABLE IF NOT EXISTS resource (
    workspace_id TEXT NOT NULL,
    path TEXT NOT NULL,
    value JSONB NOT NULL,
    resource_type TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (workspace_id, path)
);

CREATE TABLE IF NOT EXISTS resource_type (
    workspace_id TEXT NOT NULL,
    name TEXT NOT NULL,
    version TEXT NOT NULL,
    schema JSONB NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (workspace_id, name, version)
);

CREATE TABLE IF NOT EXISTS secret_access_audit (
    id BIGSERIAL PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    job_id TEXT NOT NULL,
    attempt INTEGER NOT NULL,
    app_key TEXT NOT NULL,
    action_key TEXT NOT NULL,
    path TEXT NOT NULL,
    source TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS secret_access_audit_workspace_job_created_idx
ON secret_access_audit (workspace_id, job_id, created_at DESC);

CREATE TABLE IF NOT EXISTS workspace_key (
    workspace_id TEXT PRIMARY KEY,
    key TEXT NOT NULL,
    kek_version INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS workspace_registry (
    id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    token_hash TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL DEFAULT 'system',
    updated_by TEXT NOT NULL DEFAULT 'system',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS workspace_token (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspace_registry(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL DEFAULT 'system',
    updated_by TEXT NOT NULL DEFAULT 'system',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS workspace_audit (
    id BIGSERIAL PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    detail TEXT NOT NULL DEFAULT '',
    actor TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

DO $$
BEGIN
    IF to_regclass('client_registry') IS NULL AND to_regclass('api_client') IS NOT NULL THEN
        ALTER TABLE api_client RENAME TO client_registry;
    END IF;
    IF to_regclass('client_registry_audit') IS NULL AND to_regclass('api_client_audit') IS NOT NULL THEN
        ALTER TABLE api_client_audit RENAME TO client_registry_audit;
    END IF;
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema() AND table_name = 'client_registry' AND column_name = 'client_key'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema() AND table_name = 'client_registry' AND column_name = 'token_hash'
    ) THEN
        ALTER TABLE client_registry RENAME COLUMN client_key TO token_hash;
    END IF;
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema() AND table_name = 'client_registry' AND column_name = 'external_key'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema() AND table_name = 'client_registry' AND column_name = 'token_hash'
    ) THEN
        ALTER TABLE client_registry RENAME COLUMN external_key TO token_hash;
    END IF;
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema() AND table_name = 'client_registry_audit' AND column_name = 'api_client_id'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema() AND table_name = 'client_registry_audit' AND column_name = 'client_id'
    ) THEN
        ALTER TABLE client_registry_audit RENAME COLUMN api_client_id TO client_id;
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS client_registry (
    workspace_id TEXT NOT NULL,
    id TEXT NOT NULL,
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL,
    updated_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, id)
);

ALTER TABLE client_registry DROP CONSTRAINT IF EXISTS client_registry_workspace_id_external_key_key;
ALTER TABLE client_registry DROP CONSTRAINT IF EXISTS client_registry_workspace_id_client_key_key;
ALTER TABLE client_registry DROP CONSTRAINT IF EXISTS api_client_workspace_id_client_key_key;
CREATE UNIQUE INDEX IF NOT EXISTS client_registry_active_token_idx
    ON client_registry (workspace_id, token_hash) WHERE token_hash <> '';

CREATE TABLE IF NOT EXISTS client_registry_audit (
    id BIGSERIAL PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    client_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    detail TEXT NOT NULL DEFAULT '',
    actor TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS service_principal (
    workspace_id TEXT NOT NULL,
    id TEXT NOT NULL,
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL DEFAULT '',
    scopes TEXT[] NOT NULL DEFAULT '{}',
    allowed_targets TEXT[] NOT NULL DEFAULT '{}',
    created_by TEXT NOT NULL,
    updated_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, id)
);

CREATE UNIQUE INDEX IF NOT EXISTS service_principal_active_token_idx
    ON service_principal (workspace_id, token_hash) WHERE token_hash <> '';

CREATE TABLE IF NOT EXISTS service_principal_audit (
    id BIGSERIAL PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    service_principal_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    detail TEXT NOT NULL DEFAULT '',
    actor TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS input_config (
    workspace_id TEXT NOT NULL,
    app_key TEXT NOT NULL,
    action_key TEXT NOT NULL DEFAULT '',
    client_id TEXT,
    config JSONB NOT NULL,
    locked_keys TEXT[] NOT NULL DEFAULT '{}',
    updated_by TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (workspace_id, app_key, action_key, client_id),
    FOREIGN KEY (workspace_id, client_id)
        REFERENCES client_registry(workspace_id, id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS input_config_audit (
    id BIGSERIAL PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    app_key TEXT NOT NULL,
    action_key TEXT NOT NULL DEFAULT '',
    client_id TEXT,
    kind TEXT NOT NULL,
    detail TEXT NOT NULL DEFAULT '',
    actor TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS control_release_history (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    git_source_id TEXT NOT NULL,
    app_key TEXT NOT NULL,
    commit_sha TEXT NOT NULL,
    record JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS control_active_release (
    workspace_id TEXT NOT NULL,
    app_key TEXT NOT NULL,
    history_id TEXT,
    deployment JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (workspace_id, app_key)
);

CREATE TABLE IF NOT EXISTS control_audit (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    git_source_id TEXT NOT NULL,
    app_key TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    record JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS control_source_release_marker (
    workspace_id TEXT NOT NULL,
    git_source_id TEXT NOT NULL,
    commit_sha TEXT NOT NULL,
    released_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (workspace_id, git_source_id)
);

CREATE TABLE IF NOT EXISTS control_release_candidate (
    workspace_id TEXT NOT NULL,
    git_source_id TEXT NOT NULL,
    commit_sha TEXT NOT NULL,
    app_key TEXT NOT NULL,
    deployment JSONB NOT NULL,
    synced_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (workspace_id, git_source_id, commit_sha)
);

CREATE TABLE IF NOT EXISTS control_source_operation_lease (
    workspace_id TEXT NOT NULL,
    git_source_id TEXT NOT NULL,
    holder TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (workspace_id, git_source_id)
);

CREATE TABLE IF NOT EXISTS control_plane_event (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    subject TEXT NOT NULL,
    body JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS webhook_subscription (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    name TEXT NOT NULL,
    endpoint_encrypted JSONB NOT NULL,
    signing_secret_encrypted JSONB NOT NULL,
    event_types JSONB NOT NULL,
    app_keys JSONB NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_by TEXT NOT NULL,
    updated_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS webhook_subscription_active_name_idx
    ON webhook_subscription (workspace_id, name)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS webhook_delivery (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    event_id TEXT NOT NULL REFERENCES control_plane_event(id),
    subscription_id TEXT NOT NULL REFERENCES webhook_subscription(id),
    state TEXT NOT NULL,
    attempt INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL,
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ,
    response_status INTEGER,
    latency_ms BIGINT,
    error_summary TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    UNIQUE (event_id, subscription_id)
);

CREATE TABLE IF NOT EXISTS webhook_audit (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    subscription_id TEXT,
    delivery_id TEXT,
    kind TEXT NOT NULL,
    detail TEXT NOT NULL,
    actor TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS trigger_definition (
    workspace_id TEXT NOT NULL,
    id TEXT NOT NULL,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT false,
    app_key TEXT NOT NULL,
      action_key TEXT NOT NULL,
      credential_ref TEXT NOT NULL DEFAULT '',
      config JSONB NOT NULL DEFAULT '{}'::jsonb,
      completion JSONB NOT NULL DEFAULT '{"mode":"none"}'::jsonb,
      response JSONB NOT NULL DEFAULT '{"mode":"async"}'::jsonb,
      secret_config_encrypted JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by TEXT NOT NULL,
    updated_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ,
    PRIMARY KEY (workspace_id, id)
);

CREATE UNIQUE INDEX IF NOT EXISTS trigger_definition_active_name_lower_idx
    ON trigger_definition (workspace_id, lower(name))
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS trigger_audit (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    trigger_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    detail TEXT NOT NULL DEFAULT '',
    actor TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS trigger_delivery (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    trigger_id TEXT NOT NULL,
    delivery_id TEXT NOT NULL,
    correlation_id TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL,
    run_id TEXT,
      attempt INTEGER NOT NULL DEFAULT 1,
      error_summary TEXT NOT NULL DEFAULT '',
      scheduled_for TIMESTAMPTZ,
      completion JSONB NOT NULL DEFAULT '{"mode":"none"}'::jsonb,
      completion_state TEXT NOT NULL DEFAULT 'ignored',
      completion_attempt INTEGER NOT NULL DEFAULT 0,
      completion_next_attempt_at TIMESTAMPTZ,
      completion_lease_owner TEXT,
      completion_lease_expires_at TIMESTAMPTZ,
      completion_response_status INTEGER,
      completion_error_summary TEXT NOT NULL DEFAULT '',
      completion_completed_at TIMESTAMPTZ,
      created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, trigger_id, delivery_id)
);

CREATE TABLE IF NOT EXISTS http_route_binding (
    workspace_id TEXT NOT NULL,
    id TEXT NOT NULL,
    trigger_id TEXT NOT NULL,
    hostname TEXT NOT NULL DEFAULT '',
    path TEXT NOT NULL,
    visibility TEXT NOT NULL DEFAULT 'public',
    provider TEXT NOT NULL DEFAULT 'auto',
    state TEXT NOT NULL DEFAULT 'pending',
    public_url TEXT NOT NULL DEFAULT '',
    error_summary TEXT NOT NULL DEFAULT '',
    generation BIGINT NOT NULL DEFAULT 1,
    observed_generation BIGINT NOT NULL DEFAULT 0,
    created_by TEXT NOT NULL,
    updated_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    delete_requested_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    PRIMARY KEY (workspace_id, id)
);

CREATE UNIQUE INDEX IF NOT EXISTS http_route_binding_active_address_idx
    ON http_route_binding (workspace_id, lower(hostname), path)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS http_route_binding_audit (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    trigger_id TEXT NOT NULL,
    binding_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    detail TEXT NOT NULL DEFAULT '',
    actor TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

  ALTER TABLE trigger_delivery ADD COLUMN IF NOT EXISTS correlation_id TEXT NOT NULL DEFAULT '';
  ALTER TABLE trigger_delivery ADD COLUMN IF NOT EXISTS scheduled_for TIMESTAMPTZ;
  ALTER TABLE trigger_definition ADD COLUMN IF NOT EXISTS completion JSONB NOT NULL DEFAULT '{"mode":"none"}'::jsonb;
  ALTER TABLE trigger_definition ADD COLUMN IF NOT EXISTS response JSONB NOT NULL DEFAULT '{"mode":"async"}'::jsonb;
  ALTER TABLE trigger_delivery ADD COLUMN IF NOT EXISTS completion JSONB NOT NULL DEFAULT '{"mode":"none"}'::jsonb;
  ALTER TABLE trigger_delivery ADD COLUMN IF NOT EXISTS completion_state TEXT NOT NULL DEFAULT 'ignored';
  ALTER TABLE trigger_delivery ADD COLUMN IF NOT EXISTS completion_attempt INTEGER NOT NULL DEFAULT 0;
  ALTER TABLE trigger_delivery ADD COLUMN IF NOT EXISTS completion_next_attempt_at TIMESTAMPTZ;
  ALTER TABLE trigger_delivery ADD COLUMN IF NOT EXISTS completion_lease_owner TEXT;
  ALTER TABLE trigger_delivery ADD COLUMN IF NOT EXISTS completion_lease_expires_at TIMESTAMPTZ;
  ALTER TABLE trigger_delivery ADD COLUMN IF NOT EXISTS completion_response_status INTEGER;
  ALTER TABLE trigger_delivery ADD COLUMN IF NOT EXISTS completion_error_summary TEXT NOT NULL DEFAULT '';
  ALTER TABLE trigger_delivery ADD COLUMN IF NOT EXISTS completion_completed_at TIMESTAMPTZ;

ALTER TABLE runs ADD COLUMN IF NOT EXISTS result JSONB;
ALTER TABLE runs ADD COLUMN IF NOT EXISTS correlation_id TEXT;
ALTER TABLE runs ADD COLUMN IF NOT EXISTS env JSONB;
ALTER TABLE runs ADD COLUMN IF NOT EXISTS client_id TEXT;
ALTER TABLE runs ADD COLUMN IF NOT EXISTS principal_kind TEXT;
ALTER TABLE runs ADD COLUMN IF NOT EXISTS principal_id TEXT;
ALTER TABLE runs ADD COLUMN IF NOT EXISTS idempotency_hash TEXT;
ALTER TABLE runs ADD COLUMN IF NOT EXISTS request_fingerprint TEXT;
ALTER TABLE runs ADD COLUMN IF NOT EXISTS created_by TEXT NOT NULL DEFAULT 'system';
ALTER TABLE runs ADD COLUMN IF NOT EXISTS permissioned_as TEXT NOT NULL DEFAULT 'system';
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS canceled_by TEXT;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS canceled_reason TEXT;
ALTER TABLE job_logs ADD COLUMN IF NOT EXISTS workspace_id TEXT NOT NULL DEFAULT 'default';
ALTER TABLE workspace_key ADD COLUMN IF NOT EXISTS kek_version INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS queue_snapshot_state (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    store_epoch TEXT NOT NULL,
    revision BIGINT NOT NULL DEFAULT 0 CHECK (revision >= 0)
);

INSERT INTO queue_snapshot_state (singleton, store_epoch, revision)
VALUES (TRUE, 'store_' || md5(random()::text || clock_timestamp()::text), 0)
ON CONFLICT (singleton) DO NOTHING;

CREATE OR REPLACE FUNCTION bump_queue_snapshot_revision()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    UPDATE queue_snapshot_state SET revision = revision + 1 WHERE singleton = TRUE;
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS jobs_queue_snapshot_revision ON jobs;
CREATE TRIGGER jobs_queue_snapshot_revision
AFTER INSERT OR UPDATE OR DELETE ON jobs
FOR EACH STATEMENT EXECUTE FUNCTION bump_queue_snapshot_revision();

CREATE INDEX IF NOT EXISTS jobs_claim_idx
    ON jobs (priority, created_at)
    WHERE state = 'queued';

CREATE INDEX IF NOT EXISTS jobs_claim_tag_idx
    ON jobs ((NULLIF(btrim(payload->>'tag'), '')), priority, created_at, id)
    WHERE state = 'queued';

CREATE INDEX IF NOT EXISTS jobs_running_app_idx
    ON jobs (
        (COALESCE(NULLIF(payload->>'workspace', ''), NULLIF(payload->'deployment'->>'workspace', ''), 'default')),
        (COALESCE(NULLIF(payload->>'app', ''), NULLIF(payload->'deployment'->>'app', ''), ''))
    )
    WHERE state = 'running';

CREATE INDEX IF NOT EXISTS jobs_lease_idx
    ON jobs (lease_expires_at)
    WHERE state = 'running';

CREATE INDEX IF NOT EXISTS human_tasks_pending_idx
    ON human_tasks (created_at)
    WHERE state = 'pending';

CREATE INDEX IF NOT EXISTS runs_correlation_id_idx
    ON runs (correlation_id)
    WHERE correlation_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS client_registry_audit_client_idx
    ON client_registry_audit (workspace_id, client_id, created_at DESC);

CREATE INDEX IF NOT EXISTS input_config_lookup_idx
    ON input_config (workspace_id, app_key, action_key, client_id);

CREATE INDEX IF NOT EXISTS input_config_audit_lookup_idx
    ON input_config_audit (workspace_id, app_key, client_id, created_at DESC);

CREATE INDEX IF NOT EXISTS control_release_history_source_idx
    ON control_release_history (workspace_id, git_source_id, created_at DESC);

CREATE INDEX IF NOT EXISTS control_release_history_app_idx
    ON control_release_history (workspace_id, app_key, created_at DESC);

CREATE INDEX IF NOT EXISTS control_release_candidate_latest_idx
    ON control_release_candidate (workspace_id, git_source_id, synced_at DESC);

CREATE INDEX IF NOT EXISTS control_audit_source_idx
    ON control_audit (workspace_id, git_source_id, created_at DESC);

CREATE INDEX IF NOT EXISTS control_plane_event_lookup_idx
    ON control_plane_event (workspace_id, event_type, created_at DESC);

CREATE INDEX IF NOT EXISTS webhook_delivery_claim_idx
    ON webhook_delivery (state, next_attempt_at, created_at);

CREATE INDEX IF NOT EXISTS webhook_delivery_lease_idx
    ON webhook_delivery (lease_expires_at)
    WHERE state = 'delivering';

CREATE INDEX IF NOT EXISTS webhook_delivery_subscription_idx
    ON webhook_delivery (workspace_id, subscription_id, created_at DESC);

CREATE INDEX IF NOT EXISTS webhook_delivery_retention_idx
    ON webhook_delivery (state, completed_at, updated_at, id)
    WHERE state IN ('succeeded', 'failed', 'canceled');

CREATE TABLE IF NOT EXISTS worker_registry (
    id                text PRIMARY KEY,
    worker_group      text NOT NULL DEFAULT '',
    tags              jsonb NOT NULL DEFAULT '[]'::jsonb,
    labels            jsonb NOT NULL DEFAULT '[]'::jsonb,
    slots             integer NOT NULL DEFAULT 1,
    status            text NOT NULL DEFAULT 'active',
    started_at        timestamptz NOT NULL,
    last_heartbeat_at timestamptz NOT NULL
);

ALTER TABLE worker_registry
    ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'active';

CREATE INDEX IF NOT EXISTS webhook_audit_workspace_idx
    ON webhook_audit (workspace_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS trigger_audit_lookup_idx
    ON trigger_audit (workspace_id, trigger_id, created_at DESC, id DESC);

  CREATE INDEX IF NOT EXISTS trigger_delivery_lookup_idx
      ON trigger_delivery (workspace_id, trigger_id, updated_at DESC, id DESC);

  CREATE INDEX IF NOT EXISTS trigger_delivery_completion_claim_idx
      ON trigger_delivery (completion_next_attempt_at, created_at, id)
      WHERE completion_state IN ('pending', 'retrying');

  CREATE INDEX IF NOT EXISTS trigger_delivery_completion_lease_idx
      ON trigger_delivery (completion_lease_expires_at, id)
      WHERE completion_state = 'delivering';

CREATE INDEX IF NOT EXISTS http_route_binding_trigger_idx
    ON http_route_binding (workspace_id, trigger_id, created_at, id);

CREATE INDEX IF NOT EXISTS http_route_binding_provider_idx
    ON http_route_binding (workspace_id, provider, state, updated_at, id);

CREATE INDEX IF NOT EXISTS http_route_binding_audit_lookup_idx
    ON http_route_binding_audit (workspace_id, trigger_id, binding_id, created_at, id);

CREATE INDEX IF NOT EXISTS workspace_audit_lookup_idx
    ON workspace_audit (workspace_id, created_at DESC, id DESC);

CREATE UNIQUE INDEX IF NOT EXISTS workspace_token_hash_unique_idx
    ON workspace_token (token_hash) WHERE token_hash <> '';

CREATE INDEX IF NOT EXISTS workspace_token_workspace_idx
    ON workspace_token (workspace_id, created_at DESC, id DESC);

INSERT INTO workspace_registry (id, display_name, status, created_by, updated_by)
VALUES ('default', 'Default', 'active', 'system', 'system')
ON CONFLICT (id) DO NOTHING;

INSERT INTO workspace_registry (id, display_name, status, created_by, updated_by)
SELECT workspace_id, workspace_id, 'active', 'system', 'system'
FROM (
    SELECT workspace_id FROM job_state
    UNION SELECT workspace_id FROM variable
    UNION SELECT workspace_id FROM resource
    UNION SELECT workspace_id FROM client_registry
    UNION SELECT workspace_id FROM input_config
    UNION SELECT workspace_id FROM control_release_history
    UNION SELECT workspace_id FROM webhook_subscription
    UNION SELECT workspace_id FROM trigger_definition
    UNION SELECT workspace_id FROM http_route_binding
    UNION SELECT COALESCE(NULLIF(payload->>'workspace', ''), 'default') FROM jobs
) discovered
WHERE workspace_id <> ''
ON CONFLICT (id) DO NOTHING;

INSERT INTO workspace_token (
    id, workspace_id, name, token_hash, created_by, updated_by, created_at, updated_at
)
SELECT
    'workspace_token_legacy_' || md5(id),
    id,
    'Legacy access token',
    token_hash,
    created_by,
    updated_by,
    created_at,
    updated_at
FROM workspace_registry
WHERE token_hash <> ''
ON CONFLICT (id) DO NOTHING;

UPDATE workspace_registry SET token_hash='' WHERE token_hash <> '';
`); err != nil {
		return err
	}
	const clientTokenMigration = "client-token-prefix-v1"
	var applied bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migration WHERE name=$1)`, clientTokenMigration).Scan(&applied); err != nil {
		return err
	}
	if !applied {
		if _, err := tx.Exec(ctx, `
INSERT INTO client_registry_audit (workspace_id, client_id, kind, detail, actor)
SELECT workspace_id, id, 'token_revoked_migration', 'issue a wfk_ token before using the public API', 'system:migration'
FROM client_registry
WHERE token_hash <> ''`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
UPDATE client_registry
SET token_hash='', updated_by='system:migration', updated_at=now()
WHERE token_hash <> ''`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migration (name) VALUES ($1)`, clientTokenMigration); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
