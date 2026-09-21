-- ЧЕРНОВИК (backend-plan.md §5, §12) — см. заголовок 0001_platform.up.sql.
-- Статусы job'ов не входят в канонические enum'ы ТЗ (backend-plan.md §4.1), поэтому без CHECK —
-- уточняются в internal/platform/kafka + cmd/engine при реализации M2-M3.

CREATE TABLE parse_jobs (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    process_id  uuid NOT NULL REFERENCES processes (id),
    file_id     uuid NOT NULL REFERENCES files (id),
    page_from   integer,
    page_to     integer,
    status      text NOT NULL DEFAULT 'PENDING',
    attempts    integer NOT NULL DEFAULT 0,
    quality     text CHECK (quality IN ('OK', 'LOW_QUALITY', 'ABSTAIN', 'FAILED')),
    layout_key  text,
    started_at  timestamptz,
    finished_at timestamptz,
    last_error  text
);
CREATE INDEX idx_parse_jobs_process_id ON parse_jobs (process_id);
CREATE INDEX idx_parse_jobs_status ON parse_jobs (status);

CREATE TABLE extract_jobs (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    process_id  uuid NOT NULL REFERENCES processes (id),
    params      text[] NOT NULL DEFAULT '{}',
    status      text NOT NULL DEFAULT 'PENDING',
    attempts    integer NOT NULL DEFAULT 0,
    facts_key   text,
    started_at  timestamptz,
    finished_at timestamptz,
    last_error  text
);
CREATE INDEX idx_extract_jobs_process_id ON extract_jobs (process_id);
CREATE INDEX idx_extract_jobs_status ON extract_jobs (status);
