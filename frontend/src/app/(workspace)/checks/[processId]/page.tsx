"use client";

import Link from "next/link";
import { WorkspaceBreadcrumbs } from "@/components/workspace-breadcrumbs";
import { useParams } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { apiRequest, formatDate } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import type { ProcessDetail, ProcessFilesResponse, SyncStatusResponse } from "@/lib/types";
import { ProcessStatusBadge } from "@/components/status";

const statusText: Record<string, string> = { PENDING: "Ожидает запуска", PARSING: "Обработка документов", READY: "Готова к запуску", VERIFYING: "Проверка", COMPLETED: "Результат готов", FINALIZED: "Завершена" };
export default function CheckPage() {
  const { processId } = useParams<{ processId: string }>(); const { token } = useAuth();
  const [process, setProcess] = useState<ProcessDetail | null>(null); const [files, setFiles] = useState<ProcessFilesResponse["items"]>([]); const [sync, setSync] = useState<SyncStatusResponse | null>(null); const [busy, setBusy] = useState(false); const [error, setError] = useState("");
  const load = useCallback(async () => { if (!token) return; try { const [p, f] = await Promise.all([apiRequest<ProcessDetail>(`/processes/${processId}`, token), apiRequest<ProcessFilesResponse>(`/processes/${processId}/files`, token)]); setProcess(p); setFiles(f.items || []); if (p.status === "FINALIZED") setSync(await apiRequest<SyncStatusResponse>(`/processes/${processId}/sync`, token)); } catch (e) { setError(e instanceof Error ? e.message : "Не удалось загрузить проверку."); } }, [processId, token]);
  useEffect(() => { void load(); }, [load]);
  useEffect(() => { if (!process || !["PENDING", "PARSING", "VERIFYING"].includes(process.status)) return; const timer = window.setInterval(() => void load(), 2500); return () => window.clearInterval(timer); }, [process, load]);
  useEffect(() => { if (!token || process?.status !== "FINALIZED" || sync?.sync_status === "SYNCED") return; const refresh = () => apiRequest<SyncStatusResponse>(`/processes/${processId}/sync`, token).then(setSync).catch(() => undefined); const timer = window.setInterval(refresh, 4000); return () => window.clearInterval(timer); }, [process?.status, processId, sync?.sync_status, token]);
  async function action(path: string, body?: unknown) { if (!token) return; setBusy(true); setError(""); try { await apiRequest(`/processes/${processId}/${path}`, token, { method: "POST", ...(body ? { body: JSON.stringify(body) } : {}) }); await load(); } catch (e) { setError(e instanceof Error ? e.message : "Действие не выполнено."); } finally { setBusy(false); } }
  const accepted = files.filter(f => f.check_status === "ACCEPTED").length;
  const currentAccepted = files.filter(f => f.check_status === "ACCEPTED" && f.is_current).length;
  const needsRegistry = Boolean(process && process.status === "PENDING" && accepted > 0 && currentAccepted === 0);
  return <>
    <WorkspaceBreadcrumbs processId={processId} current="Проверка документов"/>
    <div className="page-head"><div><div className="page-eyebrow">РАБОЧИЙ ПРОЦЕСС</div><h1 className="page-title">Проверка документов</h1><p className="page-subtitle">Система сопоставляет загруженные документы с Матрицей требований.</p></div>{process && <ProcessStatusBadge status={process.status}/>}</div>
    {error && <div className="alert alert-error"><span>!</span>{error}</div>}
    {!process ? <div className="surface loading-card">Загружаем данные проверки…</div> : <>
      {process.load_warnings?.length ? <div className="warning-stack">{process.load_warnings.map(w => <div className={`load-warning ${w.severity}`} key={w.code + w.message}><span>!</span><div><strong>{w.severity === "error" ? "Проверка ограничена" : "Комплект загружен не полностью"}</strong><p>{w.message}</p></div></div>)}</div> : null}
      <section className="surface check-overview"><div className="check-overview-main"><span className="check-orb">{process.status === "FINALIZED" ? "✓" : "⌁"}</span><div><div className="page-eyebrow">{statusText[process.status] || process.status}</div><h2>{process.status === "PARSING" || process.status === "VERIFYING" ? "Идет обработка комплекта" : process.status === "FINALIZED" ? "Проверка завершена" : "Проверка готова к работе"}</h2><p>{process.status === "PARSING" || process.status === "VERIFYING" ? "Страница обновляется автоматически. Обычно это занимает несколько минут." : "Процесс создан " + formatDate(process.created_at, true)}</p></div></div>
      <div className="check-progress"><span className={process.status !== "PENDING" ? "done" : ""}>Документы загружены</span><i/><span className={["PARSING", "VERIFYING", "COMPLETED", "FINALIZED"].includes(process.status) ? "done" : ""}>Анализ выполнен</span><i/><span className={["COMPLETED", "FINALIZED"].includes(process.status) ? "done" : ""}>Решения проверены</span><i/><span className={process.status === "FINALIZED" ? "done" : ""}>Протокол</span></div>
      <div className="check-facts"><div><small>Создана</small><strong>{formatDate(process.created_at, true)}</strong></div><div><small>Документы</small><strong>{accepted} из {files.length} приняты · {currentAccepted} актуальных</strong></div><div><small>Сценарий</small><strong>{process.scenario || "Определяется по комплекту"}</strong></div><div><small>Версия матрицы</small><strong>{process.matrix_version || "—"}</strong></div></div>
      {needsRegistry && <div className="alert alert-warning registry-required"><span>!</span><div><strong>Нужно заполнить реестр документов</strong><p>Файлы загружены, но система пока не знает их стадию и актуальную редакцию. Без этого проверка не запустится и протокол не сформируется. Уже загружать документы заново не нужно.</p><Link className="button button-secondary" href={`/objects/${process.object_id}/checks/new?processId=${processId}&step=registry`}>Продолжить с реестром <span>→</span></Link></div></div>}
      {process.status === "PENDING" && <div className="check-cta"><p>{currentAccepted > 0 ? "Все готово. Запустите анализ, чтобы получить список расхождений и подтверждающие фрагменты." : "После определения актуальных документов можно будет запустить анализ."}</p><button className="button button-primary" disabled={busy || currentAccepted === 0} onClick={() => void action("start")}>{busy ? "Запускаем…" : <>Запустить проверку <span>→</span></>}</button></div>}
      {(["PARSING", "VERIFYING"].includes(process.status)) && <div className="processing-note"><span className="spinner"/> Обрабатываем комплект · обновление статуса каждые несколько секунд</div>}
      {process.status === "COMPLETED" && <div className="check-cta"><p>Анализ завершен. Проверьте кандидатов и решения в протоколе, затем финализируйте результат.</p><Link className="button button-primary" href={`/checks/${processId}/protocol`}>Открыть протокол <span>→</span></Link></div>}
      {process.status === "FINALIZED" && <div className="sync-panel"><span className="sync-mark">{sync?.sync_status === "SYNCED" ? "✓" : "↻"}</span><div><strong>{sync?.sync_status === "SYNCED" ? "Передано в ИАИС «РиН»" : sync?.sync_status === "SYNC_FAILED" ? "Синхронизация ожидает повтора" : "Синхронизация с ИАИС «РиН»"}</strong><small>{sync?.sync_status === "SYNC_FAILED" ? "Система повторит отправку автоматически. Если ошибка сохраняется, проверьте статус позже." : sync?.updated_at ? "Обновлено " + formatDate(sync.updated_at, true) : "Статус обновляется автоматически."}</small></div><Link href={`/checks/${processId}/protocol`}>Протокол →</Link></div>}
      <div className="check-links"><Link href={`/checks/${processId}/documents`}><span>▤</span><div><strong>Документы проверки</strong><small>Состав комплекта и результаты приема</small></div><b>→</b></Link>{["COMPLETED", "FINALIZED"].includes(process.status) ? <Link href={`/checks/${processId}/protocol`}><span>☷</span><div><strong>Протокол проверки</strong><small>Расхождения, доказательства и решения</small></div><b>→</b></Link> : <div className="check-link-locked" aria-disabled="true"><span>☷</span><div><strong>Протокол проверки</strong><small>{needsRegistry ? "Сформируется после реестра и анализа" : "Сформируется после завершения анализа"}</small></div><b>⌑</b></div>}</div>
      <div className="check-footnote">Проверка использует только документы, принятые системой. Предупреждения о неполном комплекте влияют на интерпретацию результата.</div>
      {process.status === "FINALIZED" && <div className="muted-line">Финализировано {formatDate(process.finalized_at, true)}.</div>}
    </section></>}
  </>;
}
