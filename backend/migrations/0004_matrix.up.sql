-- ЧЕРНОВИК (backend-plan.md §5, §12) — см. заголовок 0001_platform.up.sql.
-- Наполняется через cmd/tools/import-matrix из xlsx (backend-plan.md §11, M2); code — канонический
-- код 'M-001'..'M-132', alias_code — код из примеров ТЗ ('KR-55' и т.п., backend-plan.md §12).

CREATE TABLE params (
    id              integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    code            varchar(20) NOT NULL UNIQUE,
    alias_code      varchar(20),
    section         text,
    parameter_name  text NOT NULL,
    unit            text,
    source_pd       text,
    source_rd       text,
    source_id       text,
    trigger_logic   text,
    review_priority text CHECK (review_priority IN ('HIGH', 'MEDIUM', 'LOW')),
    sp_reference    text,
    gost_reference  text,
    fz_reference    text,
    other_normative text,
    data_type       text,
    min_value       double precision,
    max_value       double precision,
    regex_pattern   text,
    is_active       boolean NOT NULL DEFAULT true,
    matrix_version  text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_params_is_active ON params (is_active);

CREATE TABLE normative_base (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    document_name   text NOT NULL,
    document_number text,
    section         text,
    parameter_name  text,
    min_value       double precision,
    max_value       double precision,
    effective_from  date,
    effective_to    date
);

CREATE TABLE logical_rules (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    rule_name      text NOT NULL,
    condition      jsonb NOT NULL,
    expected       jsonb NOT NULL,
    normative_base text,
    is_active      boolean NOT NULL DEFAULT true
);
