-- ЧЕРНОВИК (backend-plan.md §5, §12): финальная схема согласуется позже, эта миграция — рабочая
-- версия для локальной разработки и проверки стенда, пока нет финальной модели данных. Смысл полей
-- и статусов не менять произвольно (backend-plan.md §4.1) — можно только уточнять типы/индексы.

CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    login         text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    full_name     text NOT NULL,
    role          text NOT NULL CHECK (role IN ('inspector', 'supervisor', 'admin', 'ml_engineer')),
    is_active     boolean NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE outbox_events (
    event_id     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    aggregate_id uuid NOT NULL,
    event_type   text NOT NULL,
    topic        text NOT NULL,
    msg_key      text NOT NULL,
    payload      jsonb NOT NULL,
    status       text NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending', 'publishing', 'published', 'failed')),
    attempts     integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT now(),
    locked_at    timestamptz,
    published_at timestamptz,
    last_error   text,
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- cmd/relay: SELECT ... FOR UPDATE SKIP LOCKED по этому индексу (architecture.md#transactional-outbox--relay).
CREATE INDEX idx_outbox_events_pending ON outbox_events (status, available_at)
    WHERE status IN ('pending', 'failed', 'publishing');

CREATE TABLE consumed_events (
    consumer_name text NOT NULL,
    event_id      uuid NOT NULL,
    processed_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (consumer_name, event_id)
);

CREATE TABLE idempotency_keys (
    key             text PRIMARY KEY,
    user_id         uuid NOT NULL REFERENCES users (id),
    request_hash    text NOT NULL,
    response_status integer,
    response_body   jsonb,
    created_at      timestamptz NOT NULL DEFAULT now(),
    expires_at      timestamptz NOT NULL
);

CREATE TABLE audit_log (
    id          bigserial PRIMARY KEY,
    user_id     uuid REFERENCES users (id),
    action      text NOT NULL,
    object_type text NOT NULL,
    object_id   text NOT NULL,
    details     jsonb,
    before      jsonb,
    after       jsonb,
    ip          inet,
    user_agent  text,
    request_id  text,
    ts          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_log_object ON audit_log (object_type, object_id);

-- Заглушка по ТЗ; фактические метрики идут в Prometheus (internal/platform/metrics), эта таблица —
-- минимальная реализация на случай, если ТЗ требует их отдельно и в БД (backend-plan.md §5).
CREATE TABLE monitoring_metrics (
    id          bigserial PRIMARY KEY,
    metric      text NOT NULL,
    value       double precision NOT NULL,
    labels      jsonb,
    recorded_at timestamptz NOT NULL DEFAULT now()
);
