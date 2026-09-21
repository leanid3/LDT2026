-- ЧЕРНОВИК (backend-plan.md §5, §12) — см. заголовок 0001_platform.up.sql.
-- violation_id в rejection_log/dispute_log трактуется как ссылка на findings (backend-plan.md не
-- уточняет FK-таргет явно) — уточнить при согласовании финальной схемы (backend-plan.md §12).

CREATE TABLE protocols (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    process_id          uuid NOT NULL REFERENCES processes (id),
    object_id           uuid NOT NULL REFERENCES objects (id),
    version             integer NOT NULL,
    status              text NOT NULL DEFAULT 'DRAFT'
                            CHECK (status IN ('DRAFT', 'VERIFICATION_COMPLETED', 'PROTOCOL_FINALIZED')),
    matrix_version      text,
    dataset_version     text,
    model_version       text,
    input_manifest_hash char(64),
    payload_key         text,
    created_at          timestamptz NOT NULL DEFAULT now(),
    finalized_at        timestamptz,
    sync_status         text NOT NULL DEFAULT 'NOT_REQUIRED'
                            CHECK (sync_status IN ('NOT_REQUIRED', 'PENDING_SYNC', 'SYNCED', 'SYNC_FAILED')),
    UNIQUE (process_id, version)
);
CREATE INDEX idx_protocols_process_id ON protocols (process_id);

CREATE TABLE rejection_log (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    violation_id      uuid NOT NULL REFERENCES findings (id),
    rejection_reason  text,
    ai_verdict        text,
    suggested_fix     text,
    retraining_status text
);
CREATE INDEX idx_rejection_log_violation_id ON rejection_log (violation_id);

CREATE TABLE dispute_log (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    violation_id      uuid NOT NULL REFERENCES findings (id),
    inspector_comment text,
    ai_comment        text,
    resolution_status text,
    resolved_by       uuid REFERENCES users (id)
);
CREATE INDEX idx_dispute_log_violation_id ON dispute_log (violation_id);
