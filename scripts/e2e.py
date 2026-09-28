#!/usr/bin/env python3
"""Сквозной сценарий «Инспектора ИИ» через публичный API (только stdlib, без зависимостей).

    логин → объект → загрузка ПД+РД (presigned POST в MinIO) → confirm → реестр → старт →
    ожидание READY (parse → extract → движок) → протокол → решения инспектора → finalize → РиН

Предусловия: стенд (`make up migrate seed`), api, relay, engine, воркеры (Python: workers/ `make run-parse
run-extract`, ЛИБО backend `make run-mockworkers` — но не оба), rin-sync и rin-mock.
Код возврата 0 — весь сценарий прошёл и итоговые проверки сошлись.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import sys
import time
import urllib.error
import urllib.request
import uuid
from pathlib import Path

FIXTURES = Path(__file__).parent / "e2e_fixtures"


class Api:
    def __init__(self, base: str) -> None:
        self.base, self.token = base.rstrip("/"), ""

    def call(self, method: str, path: str, body=None, raw: bytes | None = None, ctype: str = "application/json",
             expect: tuple[int, ...] = (200, 201)):
        data = raw if raw is not None else (json.dumps(body).encode() if body is not None else None)
        req = urllib.request.Request(self.base + path, data=data, method=method)
        if data is not None:
            req.add_header("Content-Type", ctype)
        if self.token:
            req.add_header("Authorization", f"Bearer {self.token}")
        try:
            with urllib.request.urlopen(req, timeout=60) as r:
                status, payload = r.status, r.read()
        except urllib.error.HTTPError as e:
            status, payload = e.code, e.read()
        doc = json.loads(payload) if payload else None
        if status not in expect:
            raise SystemExit(f"{method} {path} -> {status}: {payload.decode(errors='replace')[:400]}")
        return doc


def multipart(fields: dict[str, str], filename: str, content: bytes) -> tuple[bytes, str]:
    boundary = uuid.uuid4().hex
    parts = []
    for k, v in fields.items():
        parts.append(f'--{boundary}\r\nContent-Disposition: form-data; name="{k}"\r\n\r\n{v}\r\n'.encode())
    parts.append(f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="{filename}"\r\n'
                 f'Content-Type: application/octet-stream\r\n\r\n'.encode() + content + b"\r\n")
    parts.append(f"--{boundary}--\r\n".encode())
    return b"".join(parts), f"multipart/form-data; boundary={boundary}"


def step(msg: str) -> None:
    print(f"\n== {msg}", flush=True)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--api", default="http://localhost:8080/api/v1")
    ap.add_argument("--login", default="inspector1")
    ap.add_argument("--password", default="demo-password-123")  # dev-пароль из cmd/tools/seed-users
    ap.add_argument("--timeout", type=int, default=180, help="сколько ждать READY, сек")
    args = ap.parse_args()
    api = Api(args.api)

    step("логин")
    api.token = api.call("POST", "/auth/login", {"login": args.login, "password": args.password})["access_token"]
    print("роль:", api.call("GET", "/auth/me")["role"])

    step("объект")
    obj = api.call("POST", "/objects", {"name": f"E2E {time.strftime('%H:%M:%S')}", "address": "Москва, демо"})
    print("object_id:", obj["id"])

    files = {"pd_tep.pdf": ("PD", "АР"), "rd_tep.pdf": ("RD", "АР")}
    blobs = {n: (FIXTURES / n).read_bytes() for n in files}

    step("upload → MinIO (presigned POST)")
    up = api.call("POST", "/documents/upload", {"object_id": obj["id"],
                  "files": [{"original_name": n, "size_bytes": len(b)} for n, b in blobs.items()]})
    pid = up["process_id"]
    for slot in up["files"]:
        body, ctype = multipart(slot["upload_fields"], slot["original_name"], blobs[slot["original_name"]])
        req = urllib.request.Request(slot["upload_url"], data=body, method="POST", headers={"Content-Type": ctype})
        with urllib.request.urlopen(req, timeout=60) as r:
            assert r.status in (200, 201, 204), r.status
        print("  загружен", slot["original_name"])

    step("confirm (sha256, ClamAV, magic bytes, pdfcpu)")
    conf = api.call("POST", f"/documents/{pid}/confirm")
    for f in conf["files"]:
        print(" ", f["file_id"][:8], f["check_status"], "стр.", f.get("page_count"))
    assert all(f["check_status"] == "ACCEPTED" for f in conf["files"]), "не все файлы приняты"

    step("реестр: стадии и редакции")
    rows = ["object_id,file_name,sha256,doc_stage,discipline,document_code,revision,approval_status,approval_date,sheet_page_range,predecessor_ref,successor_ref,signature_status"]
    for n, (stage, disc) in files.items():
        rows.append(f"{obj['id']},{n},{hashlib.sha256(blobs[n]).hexdigest()},{stage},{disc},{stage}-АР-001,1,APPROVED,2026-01-01,1-2,,,SIGNED")
    body, ctype = multipart({}, "registry.csv", ("\n".join(rows) + "\n").encode())
    print(" ", api.call("POST", f"/documents/{pid}/registry", raw=body, ctype=ctype))

    step("старт проверки")
    api.call("POST", f"/processes/{pid}/start", expect=(200, 202))
    deadline, last = time.time() + args.timeout, ""
    while time.time() < deadline:
        proc = api.call("GET", f"/processes/{pid}")
        if proc["status"] != last:
            print("  статус:", proc["status"], "| сценарий:", proc.get("scenario"), flush=True)
            last = proc["status"]
        if proc["status"] == "READY":
            break
        time.sleep(2)
    else:
        print("ТАЙМАУТ: процесс не дошёл до READY (запущены ли relay/engine и воркеры?)")
        return 1

    step("протокол")
    proto = api.call("GET", f"/processes/{pid}/protocol")
    fs = proto["findings"]
    print(f"  версия {proto['version']}, статус {proto['status']}, findings: {len(fs)}")
    assert all(f.get("param_code") and f.get("evidence") for f in fs), "в протоколе у каждого finding должны быть параметр и доказательства"
    for f in fs:
        label = f.get("param_code") or f["check_id"][:8]
        extra = f"  {f.get('expected_value')!s:>7} → {f.get('actual_value')!s:<7}" if "expected_value" in f else ""
        print(f"  {label:<6} {f['finding_status']:<18}{extra}  {f.get('parameter_name', '')}")

    step("карточка для интерфейса: доказательства (страница, цитата, bbox) и скачивание файла")
    card = next(f for f in fs if f["finding_status"] == "CANDIDATE")
    card = api.call("GET", f"/findings/{card['id']}")
    print(f"  {card['param_code']} «{card['parameter_name']}» [{card.get('review_priority')}]: {card.get('rationale')}")
    assert len(card["evidence"]) == 2 or card["param_code"] == "M-041", f"ожидалось 2 доказательства, есть {len(card['evidence'])}"
    for ev in card["evidence"]:
        assert ev["quote"] and ev["page"] and len(ev["bbox"]) == 4 and all(0 <= v <= 1 for v in ev["bbox"]), ev
        print(f"   {ev['role']:<8} {ev['stage']} {ev['original_name']} стр.{ev['page']} bbox={ev['bbox']} «{ev['quote']}»")
    dl = api.call("GET", f"/files/{card['evidence'][0]['file_id']}/download-url")
    with urllib.request.urlopen(dl["url"], timeout=30) as r:
        head = r.read(5)
    assert head == b"%PDF-", head
    print("  ссылка на файл работает, содержимое — PDF")

    step("каталог параметров, список процессов объекта, CORS")
    catalog = api.call("GET", "/params")
    assert len(catalog["items"]) == 132, len(catalog["items"])
    print(f"  Матрица {catalog['matrix_version']}: {len(catalog['items'])} параметров")
    procs = api.call("GET", f"/objects/{obj['id']}/processes")["items"]
    assert [p["id"] for p in procs] == [pid], procs
    print(f"  процессов у объекта: {len(procs)} ({procs[0]['status']})")
    pre = urllib.request.Request(api.base + "/objects", method="OPTIONS", headers={
        "Origin": "http://localhost:5173", "Access-Control-Request-Method": "POST",
        "Access-Control-Request-Headers": "authorization,content-type"})
    with urllib.request.urlopen(pre, timeout=10) as r:
        assert r.status == 204 and r.headers["Access-Control-Allow-Origin"] == "http://localhost:5173"
    print("  CORS preflight для http://localhost:5173 — ок")

    step("решения инспектора: CANDIDATE → CONFIRMED_VIOLATION")
    candidates = [f for f in fs if f["finding_status"] == "CANDIDATE"]
    for f in candidates:
        api.call("POST", f"/findings/{f['id']}/decision", {"decision": "CONFIRMED_VIOLATION", "comment": "e2e: подтверждено"})
    pending = [f for f in fs if f["finding_status"] != "CANDIDATE"]
    print(f"  подтверждено: {len(candidates)}, прочих (без решения): {len(pending)}")
    proc = api.call("GET", f"/processes/{pid}")
    print("  статус процесса:", proc["status"])

    step("finalize → синхронизация с ИАИС «РиН»")
    api.token = api.call("POST", "/auth/login", {"login": args.login, "password": args.password})["access_token"]
    api.call("POST", f"/processes/{pid}/finalize", expect=(200, 202))
    for _ in range(60):
        sync = api.call("GET", f"/processes/{pid}/sync")["sync_status"]
        if sync in ("SYNCED", "SYNC_FAILED"):
            break
        time.sleep(2)
    print("  sync_status:", sync)

    ok = sync == "SYNCED" and api.call("GET", f"/processes/{pid}")["status"] == "FINALIZED"
    print("\nИТОГ:", "OK — весь конвейер прошёл" if ok else "ЕСТЬ ПРОБЛЕМЫ")
    print("process_id:", pid)
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
