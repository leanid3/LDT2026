-- ЧЕРНОВИК (backend-plan.md §5, §12) — см. заголовок 0001_platform.up.sql.
-- Поля листа «СХЕМА GOLD» из xlsx сверить с dataset_items при реализации M4 (backend-plan.md §12).

CREATE TABLE model_versions (
    model_version   text PRIMARY KEY,
    artifact_hash   text NOT NULL,
    dataset_version text,
    metrics_json    jsonb,
    approval_status text,
    approved_by     uuid REFERENCES users (id),
    deployed_at     timestamptz,
    rollback_to     text REFERENCES model_versions (model_version)
);

CREATE TABLE dataset_items (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    evidence_group_id uuid NOT NULL REFERENCES evidence_groups (id),
    gold_label        text,
    expert_id         uuid REFERENCES users (id),
    reason_code       text CHECK (reason_code IN ('WRONG_REVISION', 'APPROVED_CHANGE', 'OCR_ERROR', 'LINKING_ERROR',
                                                    'NOT_APPLICABLE', 'DUPLICATE', 'OTHER')),
    dataset_version   text NOT NULL,
    split             text,
    object_group_id   text,
    created_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_dataset_items_evidence_group_id ON dataset_items (evidence_group_id);
CREATE INDEX idx_dataset_items_dataset_version ON dataset_items (dataset_version);

CREATE TABLE ml_retraining_log (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    model_version        text NOT NULL REFERENCES model_versions (model_version),
    dataset_version      text NOT NULL,
    split_hashes         jsonb,
    precision            double precision,
    recall               double precision,
    f1                   double precision,
    false_positive_rate  double precision,
    per_category_metrics jsonb,
    approval_status      text,
    approved_by          uuid REFERENCES users (id)
);
