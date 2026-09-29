"use client";

import Link from "next/link";
import { WorkspaceBreadcrumbs } from "@/components/workspace-breadcrumbs";
import { useParams } from "next/navigation";
import { useCallback, useEffect, useMemo, useState } from "react";
import { apiRequest, formatDate } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import type { Finding, ProcessDetail, ProtocolResponse } from "@/lib/types";

const decisionNames: Record<string, string> = { PENDING: "Ожидает решения", CONFIRMED_VIOLATION: "Подтверждено", NEGATIVE_VERIFIED: "Расхождения нет", CLARIFICATION_REQUIRED: "Нужно уточнение" };
const priorityNames: Record<string, string> = { HIGH: "Высокий приоритет", MEDIUM: "Средний приоритет", LOW: "Низкий приоритет" };
const findingNames: Record<string, string> = { CANDIDATE: "Кандидат", NEGATIVE_VERIFIED: "Совпадение", CONFIRMED_VIOLATION: "Подтвержденное нарушение", MISSING_EVIDENCE: "Недостаточно данных", NOT_APPLICABLE: "Неприменимо", NOT_COMPARABLE: "Нельзя сравнить", CLARIFICATION_REQUIRED: "Требует уточнения", SUSPICION: "Подозрение" };
export default function ProtocolPage() {
  const { processId } = useParams<{ processId: string }>(); const { token } = useAuth(); const [protocol, setProtocol] = useState<ProtocolResponse | null>(null); const [process, setProcess] = useState<ProcessDetail | null>(null); const [filter, setFilter] = useState("ALL"); const [busy, setBusy] = useState(false); const [error, setError] = useState("");
  const load = useCallback(async () => { if (!token) return; try { const [p, d] = await Promise.all([apiRequest<ProtocolResponse>(`/processes/${processId}/protocol`, token), apiRequest<ProcessDetail>(`/processes/${processId}`, token)]); setProtocol(p); setProcess(d); } catch(e) { setError(e instanceof Error ? e.message : "Не удалось загрузить протокол."); } }, [processId, token]);
  useEffect(() => { void load(); }, [load]);
  const findings = useMemo(() => protocol?.findings || [], [protocol]); const pending = findings.filter(f => f.finding_status === "CANDIDATE" && f.inspector_status === "PENDING").length;
  const visible = useMemo(() => findings.filter(f => filter === "ALL" || (filter === "PENDING" ? f.finding_status === "CANDIDATE" && f.inspector_status === "PENDING" : filter === "DECIDED" ? f.inspector_status !== "PENDING" : f.review_priority === filter)), [findings, filter]);
  async function finalize() { if (!token) return; setBusy(true); setError(""); try { await apiRequest(`/processes/${processId}/finalize`, token, { method: "POST" }); await load(); } catch(e) { setError(e instanceof Error ? e.message : "Не удалось финализировать проверку."); } finally { setBusy(false); } }
  return <>
    <WorkspaceBreadcrumbs processId={processId} processAncestor current="Протокол"/>
    <div className="page-head"><div><div className="page-eyebrow">РЕЗУЛЬТАТ АНАЛИЗА</div><h1 className="page-title">Протокол проверки</h1><p className="page-subtitle">Проверьте выводы системы и примите решение по каждому кандидату.</p></div><span className={`protocol-state ${protocol?.status || ""}`}>{(protocol?.status === "PROTOCOL_FINALIZED" || process?.status === "FINALIZED") ? "Финализирован" : protocol?.status === "VERIFICATION_COMPLETED" ? "Проверка завершена" : "Черновик"}</span></div>
    {error && <div className="alert alert-error"><span>!</span>{error}</div>}
    {protocol?.load_warnings?.length ? <div className="warning-stack">{protocol.load_warnings.map(w => <div className={`load-warning ${w.severity}`} key={w.code + w.message}><span>!</span><div><strong>{w.severity === "error" ? "Проверка ограничена" : "Комплект загружен не полностью"}</strong><p>{w.message}</p></div></div>)}</div> : null}
    <div className="protocol-stats"><div><small>Всего выводов</small><strong>{findings.length}</strong></div><div><small>Ждут решения</small><strong className={pending ? "text-orange" : ""}>{pending}</strong></div><div><small>Подтверждено</small><strong>{findings.filter(f => f.inspector_status === "CONFIRMED_VIOLATION").length}</strong></div><div><small>Расхождений нет</small><strong>{findings.filter(f => f.inspector_status === "NEGATIVE_VERIFIED").length}</strong></div></div>
    <section className="surface protocol-panel"><div className="section-head"><div><h2>Результаты по параметрам</h2><p>Выберите строку, чтобы изучить доказательства и принять решение.</p></div><span className="count-pill">{visible.length}</span></div>
      <div className="protocol-filters">{[["ALL", "Все"], ["PENDING", "Требуют решения"], ["HIGH", "Высокий приоритет"], ["DECIDED", "Решенные"]].map(([id, label]) => <button key={id} className={filter === id ? "filter-chip active" : "filter-chip"} onClick={() => setFilter(id)}>{label}{id === "PENDING" && pending > 0 ? <b>{pending}</b> : null}</button>)}</div>
      {visible.length === 0 ? <div className="empty-state">{findings.length ? "В этом фильтре выводов нет." : "Результаты появятся после завершения анализа документов."}</div> : <div className="finding-list">{visible.map(finding => <FindingRow key={finding.id} finding={finding} processId={processId}/>)}</div>}
      <div className="protocol-bottom"><div><strong>{process?.status === "FINALIZED" ? "Протокол финализирован" : pending ? `Осталось решений: ${pending}` : "Все кандидаты проверены"}</strong><small>{protocol?.created_at ? `Версия ${protocol.version} · сформирован ${formatDate(protocol.created_at, true)}` : "Решения сохраняются в протоколе"}</small></div>{process?.status === "COMPLETED" && <button className="button button-primary" disabled={busy || pending > 0} onClick={() => void finalize()}>{busy ? "Финализируем…" : "Финализировать протокол"}</button>}</div>
    </section>
  </>;
}
function FindingRow({ finding: f, processId }: { finding: Finding; processId: string }) { const priority = f.review_priority || "LOW"; const decision = f.inspector_status || "PENDING"; return <Link href={`/checks/${processId}/protocol/${f.id}`} className="finding-row"><span className={`priority-mark ${priority.toLowerCase()}`}/><div className="finding-copy"><div className="finding-topline"><span className="param-code">{f.param_code || "Параметр"}</span><span className={`decision-pill ${f.finding_status.toLowerCase()}`}>{findingNames[f.finding_status] || f.finding_status}</span>{f.finding_status === "CANDIDATE" && <span className={`decision-pill ${decision.toLowerCase()}`}>{decisionNames[decision] || decision}</span>}</div><strong>{f.parameter_name || "Вывод автоматической проверки"}</strong><small>{priorityNames[priority]}{f.delta ? ` · отклонение ${f.delta}` : ""}</small></div><div className="finding-values"><small>Ожидалось</small><strong>{f.expected_value || "—"}</strong><small>Получено</small><strong>{f.actual_value || "—"}</strong></div><span className="row-arrow">→</span></Link>; }
