import type { ApiErrorPayload } from "./types";

const API_BASE = (process.env.NEXT_PUBLIC_API_BASE_URL || "http://localhost:8080/api/v1").replace(/\/$/, "");

export class ApiError extends Error {
  status: number;
  code?: string;
  requestId?: string;
  details?: Record<string, unknown>;
  constructor(status: number, payload: ApiErrorPayload) {
    super(payload.error?.message || "Не удалось выполнить запрос. Попробуйте еще раз.");
    this.name = "ApiError";
    this.status = status;
    this.code = payload.error?.code;
    this.requestId = payload.request_id;
    this.details = payload.error?.details;
  }
}

export async function apiRequest<T>(path: string, token?: string | null, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (token) headers.set("Authorization", "Bearer " + token);
  if (init.body && !(init.body instanceof FormData) && !headers.has("Content-Type")) headers.set("Content-Type", "application/json");
  const response = await fetch(API_BASE + path, { ...init, headers, cache: "no-store" });
  if (response.status === 401 && typeof window !== "undefined") window.dispatchEvent(new Event("inspector:unauthorized"));
  if (!response.ok) {
    let payload: ApiErrorPayload = {};
    try { payload = await response.json() as ApiErrorPayload; } catch { /* non-JSON server response */ }
    throw new ApiError(response.status, payload);
  }
  if (response.status === 204) return undefined as T;
  return await response.json() as T;
}

export async function apiDownload(path: string, token: string): Promise<Blob> {
  const response = await fetch(API_BASE + path, { headers: { Authorization: "Bearer " + token }, cache: "no-store" });
  if (response.status === 401 && typeof window !== "undefined") window.dispatchEvent(new Event("inspector:unauthorized"));
  if (!response.ok) {
    let payload: ApiErrorPayload = {};
    try { payload = await response.json() as ApiErrorPayload; } catch { /* binary error response */ }
    throw new ApiError(response.status, payload);
  }
  return response.blob();
}

export function apiBaseUrl() { return API_BASE; }

export function formatDate(value?: string, withTime = false) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return new Intl.DateTimeFormat("ru-RU", withTime ? { dateStyle: "medium", timeStyle: "short" } : { dateStyle: "medium" }).format(date);
}

export function formatBytes(bytes: number) {
  if (bytes < 1024 * 1024) return Math.max(1, Math.round(bytes / 1024)) + " КБ";
  return (bytes / 1024 / 1024).toFixed(1).replace(".", ",") + " МБ";
}

