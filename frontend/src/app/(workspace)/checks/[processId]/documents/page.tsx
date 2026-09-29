"use client";

import Link from "next/link";
import { WorkspaceBreadcrumbs } from "@/components/workspace-breadcrumbs";
import { useParams } from "next/navigation";
import { useEffect, useState } from "react";
import { apiRequest, formatDate } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import type { FileInfo, ProcessDetail, ProcessFilesResponse } from "@/lib/types";

const stageName: Record<string, string> = { PD: "Проектная документация", RD: "Рабочая документация", ID: "Исполнительная документация" };
export default function DocumentsPage() {
  const { processId } = useParams<{ processId: string }>(); const { token } = useAuth(); const [files, setFiles] = useState<FileInfo[]>([]); const [process, setProcess] = useState<ProcessDetail | null>(null); const [error, setError] = useState("");
  useEffect(() => { if (!token) return; Promise.all([apiRequest<ProcessFilesResponse>(`/processes/${processId}/files`, token), apiRequest<ProcessDetail>(`/processes/${processId}`, token)]).then(([f, p]) => { setFiles(f.items || []); setProcess(p); }).catch(e => setError(e instanceof Error ? e.message : "Не удалось загрузить список.")); }, [processId, token]);
  async function openFile(file: FileInfo) { if (!token) return; try { const result = await apiRequest<{ url: string; expires_at: string }>(`/files/${file.id}/download-url`, token); window.open(result.url, "_blank", "noopener,noreferrer"); } catch (e) { setError(e instanceof Error ? e.message : "Не удалось открыть файл."); } }
  return <>
    <WorkspaceBreadcrumbs processId={processId} processAncestor current="Документы"/>
    <div className="page-head"><div><div className="page-eyebrow">СОСТАВ КОМПЛЕКТА</div><h1 className="page-title">Документы проверки</h1><p className="page-subtitle">Состав и статус каждого файла, который система использует при анализе.</p></div><Link className="button button-secondary" href={`/checks/${processId}`}>← К проверке</Link></div>
    {error && <div className="alert alert-error"><span>!</span>{error}</div>}
    <div className="surface documents-summary"><div><small>Всего документов</small><strong>{files.length}</strong></div><div><small>Текущие редакции</small><strong>{files.filter(f => f.is_current).length}</strong></div><div><small>Статус проверки</small><strong>{process?.status || "Загрузка…"}</strong></div></div>
    <section className="surface documents-panel"><div className="section-head"><div><h2>Файлы комплекта</h2><p>Нажмите на документ, чтобы открыть его во временной ссылке.</p></div><span className="count-pill">{files.length}</span></div>
      {files.length === 0 ? <div className="empty-state">Документы пока не отображаются.</div> : <div className="file-table">{files.map(file => <button key={file.id} className="file-row" onClick={() => void openFile(file)}><span className="file-icon">▤</span><span className="file-main"><strong>{file.original_name}</strong><small>{stageName[file.doc_stage || ""] || "Стадия не определена"} · {file.document_code || "шифр не указан"}{file.revision ? ` · редакция ${file.revision}` : ""}</small></span><span className="file-meta"><small>{file.selection_status === "SELECTED" ? "В анализе" : file.selection_reason || file.selection_status || file.check_status}</small><small>{formatDate(file.uploaded_at, true)}</small></span><b>↗</b></button>)}</div>}
      <div className="alert alert-info docs-note"><span>i</span>Ссылки на документы временные и выдаются сервером по запросу.</div>
    </section>
  </>;
}
