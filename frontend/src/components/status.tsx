import type { ProcessStatus } from "@/lib/types";

const statusText: Record<ProcessStatus, string> = { PENDING: "Подготовка", PARSING: "Обработка документов", READY: "Готово к проверке", VERIFYING: "Проверка инспектором", COMPLETED: "Можно завершить", FINALIZED: "Завершена" };
const statusTone: Record<ProcessStatus, string> = { PENDING: "neutral", PARSING: "blue", READY: "teal", VERIFYING: "orange", COMPLETED: "green", FINALIZED: "dark" };
export function ProcessStatusBadge({ status }: { status: ProcessStatus }) { return <span className={"status-badge status-" + statusTone[status]}><i/>{statusText[status] || status}</span>; }
export function StageTag({ stage, status }: { stage: string; status?: string }) { return <span className={"stage-tag stage-" + stage.toLowerCase() + (status ? " stage-" + status.toLowerCase() : "")}>{stage}</span>; }
export function LoadWarningBox({ severity, children }: { severity: "info" | "warning" | "error"; children: React.ReactNode }) { return <div className={"alert alert-" + (severity === "info" ? "info" : severity)}><span>{severity === "error" ? "!" : severity === "warning" ? "△" : "i"}</span><div>{children}</div></div>; }
