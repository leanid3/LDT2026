-- ЧЕРНОВИК (backend-plan.md §5, §12) — см. заголовок 0001_platform.up.sql.

CREATE TABLE objects (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    external_id   text UNIQUE,
    name          text NOT NULL,
    address       text,
    customer      text,
    contractor    text,
    permit_number text,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE processes (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    object_id      uuid NOT NULL REFERENCES objects (id),
    status         text NOT NULL DEFAULT 'PENDING'
                       CHECK (status IN ('PENDING', 'PARSING', 'READY', 'VERIFYING', 'COMPLETED', 'FINALIZED')),
    scenario       text
                       CHECK (scenario IN ('FULL', 'PD_RD_ONLY', 'PD_ID_ONLY', 'RD_ID_ONLY', 'SINGLE_ONLY',
                                            'PARTIALLY_LOADED')),
    matrix_version text,
    created_by     uuid REFERENCES users (id),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    finalized_at   timestamptz,
    finalized_by   uuid REFERENCES users (id)
);
CREATE INDEX idx_processes_object_id ON processes (object_id);

CREATE TABLE files (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    object_id        uuid NOT NULL REFERENCES objects (id),
    process_id       uuid NOT NULL REFERENCES processes (id),
    original_name    text NOT NULL,
    storage_key      text NOT NULL,
    size_bytes       bigint NOT NULL,
    sha256           char(64) NOT NULL,
    content_type     text,
    page_count       integer,
    check_status     text NOT NULL DEFAULT 'UPLOADING'
                         CHECK (check_status IN ('UPLOADING', 'UPLOADED', 'REJECTED_FORMAT', 'REJECTED_SIZE',
                                                  'REJECTED_CORRUPTED', 'REJECTED_VIRUS', 'ACCEPTED')),
    check_error      text,
    doc_stage        text CHECK (doc_stage IN ('PD', 'RD', 'ID')),
    discipline       text,
    document_code    text,
    revision         text,
    approval_status  text CHECK (approval_status IN ('DRAFT', 'APPROVED', 'FOR_CONSTRUCTION', 'SUPERSEDED',
                                                       'CANCELLED')),
    approval_date    date,
    sheet_page_range text,
    predecessor_id   uuid REFERENCES files (id),
    successor_id     uuid REFERENCES files (id),
    signature_status text,
    is_current       boolean NOT NULL DEFAULT false,
    selection_status text,
    selection_reason text,
    uploaded_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (process_id, sha256)
);
CREATE INDEX idx_files_process_id ON files (process_id);
CREATE INDEX idx_files_object_id ON files (object_id);
CREATE INDEX idx_files_current ON files (process_id, doc_stage, discipline, document_code) WHERE is_current;

CREATE TABLE registry_entries (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    process_id   uuid NOT NULL REFERENCES processes (id),
    raw          jsonb NOT NULL,
    file_id      uuid REFERENCES files (id),
    match_status text
);
CREATE INDEX idx_registry_entries_process_id ON registry_entries (process_id);

CREATE TABLE upload_status (
    process_id uuid NOT NULL REFERENCES processes (id),
    stage      text NOT NULL CHECK (stage IN ('PD', 'RD', 'ID')),
    status     text NOT NULL CHECK (status IN ('UPLOADED', 'PARTIAL', 'MISSING')),
    PRIMARY KEY (process_id, stage)
);
