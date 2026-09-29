"use client";

import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useState } from "react";
import { apiRequest, formatDate } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import type { ObjectRecord, ProcessSummary } from "@/lib/types";
import { ProcessStatusBadge } from "@/components/status";

function BuildingGlyph({ size = 40 }: { size?: number }) { return <svg width={size} height={size} style={{ width: size, height: size, flex: "none" }} viewBox="0 0 40 40" fill="none" aria-hidden="true"><path d="M9 33V11.5L20 6l11 5.5V33M15 16h2m6 0h2M15 21h2m6 0h2M17 33v-7h6v7M5 33h30" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round"/></svg>; }
const scenarioNames: Record<string, string> = { FULL: "ПД · РД · ИД", PD_RD_ONLY: "ПД · РД", PD_ID_ONLY: "ПД · ИД", RD_ID_ONLY: "РД · ИД", SINGLE_ONLY: "Одна стадия", PARTIALLY_LOADED: "Частичный комплект" };
export default function ObjectDetailPage() {
  const params = useParams<{ objectId: string }>(); const objectId = params.objectId;
  const { token } = useAuth(); const router = useRouter();
  const [record, setRecord] = useState<ObjectRecord | null>(null); const [processes, setProcesses] = useState<ProcessSummary[]>([]); const [loading, setLoading] = useState(true); const [error, setError] = useState("");
  const refresh = useCallback(async () => { if (!token || !objectId) return; setLoading(true); setError(""); try { const [objectResult, processResult] = await Promise.all([apiRequest<ObjectRecord>("/objects/" + objectId, token), apiRequest<{ items: ProcessSummary[] }>("/objects/" + objectId + "/processes", token)]); setRecord(objectResult); setProcesses(processResult.items || []); } catch (err) { setError(err instanceof Error ? err.message : "Не удалось загрузить карточку объекта."); } finally { setLoading(false); } }, [token, objectId]);
  useEffect(() => { refresh(); }, [refresh]);
  const latest = useMemo(() => processes[0], [processes]);
  if (loading) return <div className="loading-inline"><span className="spinner"/> Загружаем карточку объекта…</div>;
  if (error || !record) return <div className="alert alert-error"><span>!</span><div>{error || "Объект не найден."}<div className="error-detail"><Link href="/objects" className="inline-link">Вернуться к объектам</Link></div></div></div>;
  return <>
    <div className="breadcrumbs"><Link href="/objects">Объекты</Link><span>/</span><strong>{record.name}</strong></div>
    <div className="object-detail-head"><div className="object-detail-title"><span className="object-icon object-icon-large"><BuildingGlyph size={27}/></span><div><div className="page-eyebrow">ОБЪЕКТ СТРОИТЕЛЬСТВА</div><h1 className="page-title">{record.name}</h1><p className="page-subtitle">{record.address || "Адрес не указан"}</p></div></div><Link className="button button-primary" href={"/objects/" + objectId + "/checks/new"}><span className="icon-plus">+</span> Новая проверка</Link></div>
    <div className="detail-meta-grid surface">
      <Meta label="Заказчик" value={record.customer}/><Meta label="Подрядчик" value={record.contractor}/><Meta label="Разрешение" value={record.permit_number}/><Meta label="Создан" value={formatDate(record.created_at)}/>
    </div>
    <div className="section-title-row"><div><div className="page-eyebrow">ИСТОРИЯ РАБОТЫ</div><h2>Проверки объекта <span className="count-pill">{processes.length}</span></h2></div><span className="section-caption">Новые проверки отображаются первыми</span></div>
    {processes.length ? <div className="process-list">{processes.map((process, index) => <Link key={process.id} className="process-row surface-flat" href={"/checks/" + process.id}>
      <span className="process-index">{String(processes.length - index).padStart(2,"0")}</span><span className="process-main"><strong>Проверка документации</strong><small>Создана {formatDate(process.created_at, true)} <i>·</i> {scenarioNames[process.scenario || ""] || "Комплект уточняется"}</small></span><ProcessStatusBadge status={process.status}/><span className="process-row-arrow">→</span>
    </Link>)}</div> : <div className="surface empty-state"><span className="empty-symbol"><BuildingGlyph size={23}/></span><h3>Проверок еще нет</h3><p>Создайте проверку и загрузите комплект документов для этого объекта.</p><Link className="button button-primary" href={"/objects/" + objectId + "/checks/new"}>Начать первую проверку</Link></div>}
    {latest && <div className="recent-hint"><span className="live-indicator"/><span>Последняя проверка</span><strong>{formatDate(latest.updated_at, true)}</strong><button className="text-button" onClick={() => router.push("/checks/" + latest.id)}>Открыть →</button></div>}
  </>;
}
function Meta({ label, value }: { label: string; value?: string }) { return <div className="detail-meta-item"><span>{label}</span><strong>{value || "Не указано"}</strong></div>; }
