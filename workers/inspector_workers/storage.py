"""MinIO: скачивание документов, чтение/запись JSON и JSONL. Бакет создаётся при старте, если его нет
(minio/mc в стенде нет, бакет создаёт backend или воркер — кто стартовал первым)."""

from __future__ import annotations

import io
import json
import tempfile
from pathlib import Path
from typing import Any

from minio import Minio
from minio.error import S3Error

from .config import Settings


class Storage:
    def __init__(self, settings: Settings) -> None:
        self._bucket = settings.minio_bucket
        self._client = Minio(
            settings.minio_endpoint,
            access_key=settings.minio_access_key,
            secret_key=settings.minio_secret_key,
            secure=settings.minio_secure,
        )

    def ensure_bucket(self) -> None:
        try:
            if not self._client.bucket_exists(self._bucket):
                self._client.make_bucket(self._bucket)
        except S3Error as e:  # гонка с другим процессом, создавшим бакет первым
            if e.code not in ("BucketAlreadyOwnedByYou", "BucketAlreadyExists"):
                raise

    def download_to_temp(self, key: str, suffix: str = "") -> Path:
        """Скачивает объект во временный файл (вызывающий удаляет)."""
        fd, name = tempfile.mkstemp(suffix=suffix)
        Path(name).unlink(missing_ok=True)
        self._client.fget_object(self._bucket, key, name)
        return Path(name)

    def get_json(self, key: str) -> Any:
        resp = self._client.get_object(self._bucket, key)
        try:
            return json.loads(resp.read().decode("utf-8"))
        finally:
            resp.close()
            resp.release_conn()

    def put_bytes(self, key: str, data: bytes, content_type: str) -> None:
        self._client.put_object(self._bucket, key, io.BytesIO(data), len(data), content_type=content_type)

    def put_json(self, key: str, doc: Any) -> None:
        self.put_bytes(key, json.dumps(doc, ensure_ascii=False).encode("utf-8"), "application/json")

    def put_jsonl(self, key: str, rows: list[dict]) -> None:
        body = "".join(json.dumps(r, ensure_ascii=False) + "\n" for r in rows).encode("utf-8")
        self.put_bytes(key, body, "application/x-ndjson")
