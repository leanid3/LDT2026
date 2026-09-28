"""Настройки воркеров из ENV. Имена совпадают с backend (DATABASE_*, BROKER_*, MINIO_*), чтобы на стенде
один и тот же .env настраивал и Go-сервисы, и воркеры."""

from __future__ import annotations

import os
from dataclasses import dataclass, field
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[2]


def _env(name: str, default: str = "") -> str:
    return os.environ.get(name, default)


@dataclass(frozen=True)
class Settings:
    db_dsn: str
    kafka_brokers: str
    minio_endpoint: str
    minio_access_key: str
    minio_secret_key: str
    minio_bucket: str
    minio_secure: bool
    contracts_dir: Path
    validate_events: bool
    max_attempts: int
    # LLM опционален: без LLM_BASE_URL extract работает только на правилах-паттернах (метод regex).
    llm_base_url: str = ""
    llm_api_key: str = ""
    llm_model: str = ""
    llm_timeout: float = 120.0
    ocr_enabled: bool = True
    extra: dict = field(default_factory=dict)

    @property
    def llm_enabled(self) -> bool:
        return bool(self.llm_base_url and self.llm_model)

    @staticmethod
    def from_env() -> "Settings":
        db = (
            f"host={_env('DATABASE_HOST', 'localhost')} port={_env('DATABASE_PORT', '5432')} "
            f"user={_env('DATABASE_USER', 'postgres')} password={_env('DATABASE_PASSWORD', 'postgres')} "
            f"dbname={_env('DATABASE_DATABASE', 'inspector')}"
        )
        return Settings(
            db_dsn=_env("WORKER_DB_DSN", db),
            kafka_brokers=_env("BROKER_BOOTSTRAP_SERVERS", "localhost:9092"),
            minio_endpoint=_env("MINIO_ENDPOINT", "localhost:9000"),
            minio_access_key=_env("MINIO_ACCESS_KEY", "minioadmin"),
            minio_secret_key=_env("MINIO_SECRET_KEY", "minioadmin"),
            minio_bucket=_env("MINIO_BUCKET", "documents"),
            minio_secure=_env("MINIO_USE_SSL", "false").lower() == "true",
            contracts_dir=Path(_env("CONTRACTS_DIR", str(_REPO_ROOT / "contracts"))),
            validate_events=_env("WORKER_VALIDATE_EVENTS", "true").lower() == "true",
            max_attempts=int(_env("WORKER_MAX_ATTEMPTS", "3")),
            llm_base_url=_env("LLM_BASE_URL").rstrip("/"),
            llm_api_key=_env("LLM_API_KEY"),
            llm_model=_env("LLM_MODEL"),
            llm_timeout=float(_env("LLM_TIMEOUT", "120")),
            ocr_enabled=_env("WORKER_OCR", "true").lower() == "true",
        )
