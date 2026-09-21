-- ЧЕРНОВИК (backend-plan.md §5, §12) — см. заголовок 0001_platform.up.sql.

CREATE TABLE facts (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    process_id        uuid NOT NULL REFERENCES processes (id),
    object_id         uuid NOT NULL REFERENCES objects (id),
    file_id           uuid NOT NULL REFERENCES files (id),
    file_sha256       char(64) NOT NULL,
    stage             text NOT NULL CHECK (stage IN ('PD', 'RD', 'ID')),
    param_code        varchar(20),
    rule_code         varchar(20),
    element_key       text,
    value_raw         text,
    value_norm        jsonb,
    unit              text,
    page              integer,
    bbox              double precision[],
    quote             text NOT NULL,
    confidence        double precision,
    method            text,
    extractor_version text,
    has_change_notice boolean NOT NULL DEFAULT false
);
CREATE INDEX idx_facts_process_id ON facts (process_id);
CREATE INDEX idx_facts_param_code ON facts (param_code);

CREATE TABLE evidence_groups (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    process_id  uuid NOT NULL REFERENCES processes (id),
    object_id   uuid NOT NULL REFERENCES objects (id),
    param_code  varchar(20),
    rule_code   varchar(20),
    element_key text,
    input_hash  char(64) NOT NULL
);
CREATE INDEX idx_evidence_groups_process_id ON evidence_groups (process_id);

CREATE TABLE checks (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    process_id          uuid NOT NULL REFERENCES processes (id),
    param_id            integer REFERENCES params (id),
    object_id           uuid NOT NULL REFERENCES objects (id),
    evidence_group_id   uuid NOT NULL REFERENCES evidence_groups (id),
    expected_value      text,
    actual_value        text,
    delta               text,
    completeness_status text,
    finding_status      text NOT NULL
                            CHECK (finding_status IN ('NEGATIVE_VERIFIED', 'CANDIDATE', 'CONFIRMED_VIOLATION',
                                                       'MISSING_EVIDENCE', 'NOT_APPLICABLE', 'NOT_COMPARABLE',
                                                       'CLARIFICATION_REQUIRED', 'SUSPICION')),
    review_priority     text CHECK (review_priority IN ('HIGH', 'MEDIUM', 'LOW')),
    rationale           text,
    risk_level          text,
    created_at          timestamptz NOT NULL DEFAULT now(),
    superseded_by       uuid REFERENCES checks (id)
);
CREATE INDEX idx_checks_process_id ON checks (process_id);
CREATE INDEX idx_checks_evidence_group_id ON checks (evidence_group_id);

CREATE TABLE evidence_fragments (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    evidence_group_id    uuid NOT NULL REFERENCES evidence_groups (id),
    file_id              uuid NOT NULL REFERENCES files (id),
    stage                text CHECK (stage IN ('PD', 'RD', 'ID')),
    sheet_page           integer,
    bbox_polygon_norm    jsonb,
    extracted_value      text,
    quote                text,
    role_expected_actual text NOT NULL CHECK (role_expected_actual IN ('expected', 'actual'))
);
CREATE INDEX idx_evidence_fragments_group_id ON evidence_fragments (evidence_group_id);

CREATE TABLE findings (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    check_id          uuid NOT NULL REFERENCES checks (id),
    parent_finding_id uuid REFERENCES findings (id),
    finding_status    text NOT NULL
                          CHECK (finding_status IN ('NEGATIVE_VERIFIED', 'CANDIDATE', 'CONFIRMED_VIOLATION',
                                                     'MISSING_EVIDENCE', 'NOT_APPLICABLE', 'NOT_COMPARABLE',
                                                     'CLARIFICATION_REQUIRED', 'SUSPICION')),
    inspector_status  text NOT NULL DEFAULT 'PENDING'
                          CHECK (inspector_status IN ('PENDING', 'CONFIRMED_VIOLATION', 'NEGATIVE_VERIFIED',
                                                       'CLARIFICATION_REQUIRED')),
    decided_by        uuid REFERENCES users (id),
    decided_at        timestamptz,
    reason_code       text CHECK (reason_code IN ('WRONG_REVISION', 'APPROVED_CHANGE', 'OCR_ERROR', 'LINKING_ERROR',
                                                    'NOT_APPLICABLE', 'DUPLICATE', 'OTHER')),
    comment           text,
    version           integer NOT NULL DEFAULT 1
);
CREATE INDEX idx_findings_check_id ON findings (check_id);
CREATE INDEX idx_findings_inspector_status ON findings (inspector_status);

CREATE TABLE suspicions (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    process_id       uuid NOT NULL REFERENCES processes (id),
    object_id        uuid NOT NULL REFERENCES objects (id),
    discovery_method text,
    confidence       double precision,
    description      text,
    pd_reference     text,
    rd_reference     text,
    review_priority  text CHECK (review_priority IN ('HIGH', 'MEDIUM', 'LOW')),
    normative_base   text,
    finding_status   text NOT NULL DEFAULT 'SUSPICION' CHECK (finding_status = 'SUSPICION'),
    inspector_status text NOT NULL DEFAULT 'PENDING'
                         CHECK (inspector_status IN ('PENDING', 'CONFIRMED_VIOLATION', 'NEGATIVE_VERIFIED',
                                                      'CLARIFICATION_REQUIRED')),
    evidence         jsonb
);
CREATE INDEX idx_suspicions_process_id ON suspicions (process_id);
