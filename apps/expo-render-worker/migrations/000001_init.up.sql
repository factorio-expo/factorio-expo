-- Render worker database: internal execution queue and temporary artifact
-- metadata. Never read by the API; never public durable data.

CREATE TABLE runner_jobs (
    id                      text PRIMARY KEY,
    blueprint               text NOT NULL,
    state                   text NOT NULL CHECK (state IN ('queued', 'running', 'succeeded', 'failed')),
    claimed_by              text,
    lease_expires_at        timestamptz,
    failure_code            text NOT NULL DEFAULT '',
    failure_detail          text NOT NULL DEFAULT '',
    requested_preview       boolean NOT NULL DEFAULT true,
    requested_analysis      boolean NOT NULL DEFAULT true,
    preview_status          text NOT NULL DEFAULT 'pending'
                              CHECK (preview_status IN ('not_requested', 'pending', 'succeeded', 'failed')),
    preview_failure_code    text NOT NULL DEFAULT '',
    preview_failure_detail  text NOT NULL DEFAULT '',
    analysis_status         text NOT NULL DEFAULT 'pending'
                              CHECK (analysis_status IN ('not_requested', 'pending', 'succeeded', 'failed')),
    analysis_failure_code   text NOT NULL DEFAULT '',
    analysis_failure_detail text NOT NULL DEFAULT '',
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now(),
    available_at            timestamptz NOT NULL DEFAULT now(),
    -- Client-supplied Idempotency-Key; NULLs are distinct, so keyless jobs never collide.
    idempotency_key         text UNIQUE
);

CREATE INDEX idx_runner_jobs_fifo ON runner_jobs (state, available_at, created_at, id);
CREATE INDEX idx_runner_jobs_running ON runner_jobs (state) WHERE state = 'running';

CREATE TABLE runner_artifacts (
    job_id        text NOT NULL REFERENCES runner_jobs (id) ON DELETE CASCADE,
    phase         text NOT NULL CHECK (phase IN ('preview', 'analysis')),
    relative_path text NOT NULL,
    state         text NOT NULL CHECK (state IN ('available', 'deleted', 'expired')),
    media_type    text NOT NULL,
    size          bigint NOT NULL,
    sha256        text NOT NULL,
    width         integer NOT NULL,
    height        integer NOT NULL,
    created_at    timestamptz NOT NULL,
    expires_at    timestamptz NOT NULL,
    deleted_at    timestamptz,
    PRIMARY KEY (job_id, phase)
);

CREATE INDEX idx_runner_artifacts_expiry ON runner_artifacts (state, expires_at);
