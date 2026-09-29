export type Role = "inspector" | "supervisor" | "admin" | "ml_engineer";
export type ProcessStatus = "PENDING" | "PARSING" | "READY" | "VERIFYING" | "COMPLETED" | "FINALIZED";
export type DocStage = "PD" | "RD" | "ID";
export type FindingStatus = "NEGATIVE_VERIFIED" | "CANDIDATE" | "CONFIRMED_VIOLATION" | "MISSING_EVIDENCE" | "NOT_APPLICABLE" | "NOT_COMPARABLE" | "CLARIFICATION_REQUIRED" | "SUSPICION";
export type InspectorStatus = "PENDING" | "CONFIRMED_VIOLATION" | "NEGATIVE_VERIFIED" | "CLARIFICATION_REQUIRED";
export type ReasonCode = "WRONG_REVISION" | "APPROVED_CHANGE" | "OCR_ERROR" | "LINKING_ERROR" | "NOT_APPLICABLE" | "DUPLICATE" | "OTHER";

export interface User { id: string; login: string; full_name: string; role: Role; is_active: boolean }
export interface ObjectRecord { id: string; external_id?: string; name: string; address?: string; customer?: string; contractor?: string; permit_number?: string; created_at: string }
export interface ObjectList { items: ObjectRecord[] }
export interface ProcessSummary { id: string; object_id: string; status: ProcessStatus; scenario?: string; created_at: string; updated_at: string; finalized_at?: string }
export interface UploadStatusEntry { stage: DocStage; status: "UPLOADED" | "PARTIAL" | "MISSING" }
export interface LoadWarning { code: string; severity: "info" | "warning" | "error"; stage?: DocStage; count?: number; message: string }
export interface ProcessDetail { id: string; object_id: string; status: ProcessStatus; scenario?: string; matrix_version?: string; upload_status: UploadStatusEntry[]; created_at: string; updated_at: string; finalized_at?: string; load_warnings?: LoadWarning[] }
export interface FileInfo { id: string; original_name: string; doc_stage?: DocStage; discipline?: string; document_code?: string; revision?: string; approval_status?: string; is_current: boolean; selection_status?: string; selection_reason?: string; check_status: string; uploaded_at: string }
export interface ProcessFilesResponse { items: FileInfo[] }
export interface EvidenceFragment { file_id: string; original_name?: string; stage?: DocStage; page?: number; bbox?: [number, number, number, number]; quote?: string; extracted_value?: string; role: "expected" | "actual" }
export interface Finding { id: string; check_id: string; parent_finding_id?: string; finding_status: FindingStatus; inspector_status: InspectorStatus; decided_by?: string; decided_at?: string; reason_code?: ReasonCode; comment?: string; version: number; param_code?: string; parameter_name?: string; unit?: string; review_priority?: "HIGH" | "MEDIUM" | "LOW"; expected_value?: string; actual_value?: string; delta?: string; rationale?: string; evidence?: EvidenceFragment[] }
export interface ProtocolResponse { process_id: string; version: number; status: "DRAFT" | "VERIFICATION_COMPLETED" | "PROTOCOL_FINALIZED"; matrix_version?: string; dataset_version?: string; model_version?: string; created_at?: string; finalized_at?: string; scenario?: string; upload_status?: UploadStatusEntry[]; load_warnings?: LoadWarning[]; findings: Finding[] }
export interface Param { code: string; section?: string; name: string; unit?: string; review_priority?: "HIGH" | "MEDIUM" | "LOW"; data_type?: "number" | "ordinal" | "string"; source_pd?: string; source_rd?: string; source_id?: string; trigger_logic?: string }
export interface ParamList { matrix_version: string; items: Param[] }
export interface ApiErrorPayload { error?: { code?: string; message?: string; details?: Record<string, unknown> }; request_id?: string }
export interface UploadSlot { file_id: string; original_name: string; upload_url: string; upload_fields: Record<string, string>; expires_at: string }
export interface DocumentsUploadResponse { process_id: string; files: UploadSlot[] }
export interface ConfirmedFile { file_id: string; check_status: string; check_error?: string; page_count?: number }
export interface RegistryUploadResponse { total_rows: number; matched: number; unmatched: number; invalid?: number; errors?: string[]; warnings?: string[] }
export interface SyncStatusResponse { sync_status: "NOT_REQUIRED" | "PENDING_SYNC" | "SYNCED" | "SYNC_FAILED"; updated_at?: string }
