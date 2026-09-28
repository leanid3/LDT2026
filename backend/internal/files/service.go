package files

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/process"
)

type RegistryUploadResult struct {
	TotalRows int
	Matched   int // строка применена к файлу
	Unmatched int // строка корректна, но ни один загруженный файл ей не соответствует
	Invalid   int // строка отклонена из-за недопустимых значений (не применялась)
	Errors    []string
	Warnings  []string // строка применена, но что-то не заполнено (файл не сможет быть эталоном и т.п.)
}

// UploadRegistry — backend-plan.md §8.2: разбор реестра, сопоставление с файлами (по sha256, иначе по
// имени), пересчёт выбора актуальной редакции и статусов загрузки по стадии.
func (s *Service) UploadRegistry(ctx context.Context, processID uuid.UUID, filename string, r io.Reader) (RegistryUploadResult, error) {
	rows, err := ParseRegistry(filename, r)
	if err != nil {
		return RegistryUploadResult{}, fmt.Errorf("%w: %w", ErrValidation, err)
	}
	result := RegistryUploadResult{TotalRows: len(rows)}

	err = s.database.WithTx(ctx, func(tx pgx.Tx) error {
		txRepo := NewRepository(tx)

		allFiles, err := txRepo.ListByProcess(ctx, processID)
		if err != nil {
			return err
		}

		bySHA := make(map[string]File, len(allFiles))
		byName := make(map[string]File, len(allFiles))
		for _, f := range allFiles {
			if f.SHA256 != nil {
				bySHA[*f.SHA256] = f
			}
			byName[f.OriginalName] = f
		}

		resolveRef := func(ref string) *uuid.UUID {
			if ref == "" {
				return nil
			}
			if f, ok := bySHA[ref]; ok {
				return &f.ID
			}
			if f, ok := byName[ref]; ok {
				return &f.ID
			}
			return nil
		}

		unmatchedStages := make(map[string]bool)

		for i, sourceRow := range rows {
			line := i + 2 // как в таблице: строка 1 — заголовок
			row, issues := NormalizeRegistryRow(sourceRow)

			var warnings []string
			invalid := false
			for _, is := range issues {
				msg := fmt.Sprintf("строка %d, %s: %s", line, is.Field, is.Message)
				if is.Fatal {
					result.Errors = append(result.Errors, msg)
					invalid = true
				} else {
					warnings = append(warnings, msg)
				}
			}
			if invalid {
				// Строка с недопустимыми значениями не применяется целиком (частично применённая строка хуже
				// непримененной: стадия без шифра дала бы неверную группировку). Остальные строки обрабатываются.
				result.Invalid++
				rawInvalid, err := json.Marshal(sourceRow)
				if err != nil {
					return fmt.Errorf("marshal registry row: %w", err)
				}
				if _, err := tx.Exec(ctx, `
					INSERT INTO registry_entries (id, process_id, raw, file_id, match_status) VALUES ($1, $2, $3, NULL, 'INVALID')
				`, uuid.New(), processID, rawInvalid); err != nil {
					return fmt.Errorf("insert registry_entries: %w", err)
				}
				continue
			}

			var matched *File
			if row.SHA256 != "" {
				if f, ok := bySHA[row.SHA256]; ok {
					matched = &f
				}
			}
			if matched == nil && row.FileName != "" {
				if f, ok := byName[row.FileName]; ok {
					matched = &f
				}
			}

			raw, err := json.Marshal(row)
			if err != nil {
				return fmt.Errorf("marshal registry row: %w", err)
			}

			var fileIDPtr *uuid.UUID
			matchStatus := "UNMATCHED"
			if matched != nil {
				fileIDPtr = &matched.ID
				matchStatus = "MATCHED"
				result.Matched++

				predID, succID := resolveRef(row.PredecessorRef), resolveRef(row.SuccessorRef)
				if row.PredecessorRef != "" && predID == nil {
					warnings = append(warnings, fmt.Sprintf("строка %d, predecessor_ref: «%s» не найден среди загруженных файлов", line, row.PredecessorRef))
				}
				if row.SuccessorRef != "" && succID == nil {
					warnings = append(warnings, fmt.Sprintf("строка %d, successor_ref: «%s» не найден среди загруженных файлов", line, row.SuccessorRef))
				}
				if err := txRepo.UpdateRegistryFields(ctx, matched.ID, row, predID, succID); err != nil {
					return err
				}
				result.Warnings = append(result.Warnings, warnings...)
			} else {
				result.Unmatched++
				result.Errors = append(result.Errors, fmt.Sprintf("строка %d: файл «%s» не найден среди загруженных (имя должно совпадать точно, либо укажите sha256)", line, row.FileName))
				if row.DocStage != "" {
					unmatchedStages[row.DocStage] = true
				}
			}

			if _, err := tx.Exec(ctx, `
				INSERT INTO registry_entries (id, process_id, raw, file_id, match_status)
				VALUES ($1, $2, $3, $4, $5)
			`, uuid.New(), processID, raw, fileIDPtr, matchStatus); err != nil {
				return fmt.Errorf("insert registry_entries: %w", err)
			}
		}

		updatedFiles, err := txRepo.ListByProcess(ctx, processID)
		if err != nil {
			return err
		}

		if len(rows) == 0 {
			// Нет реестра у процесса → все файлы CLARIFICATION_REQUIRED (backend-plan.md §8.2).
			for _, f := range updatedFiles {
				if err := txRepo.UpdateSelection(ctx, f.ID, false, SelectionStatusClarificationRequired, "REGISTRY_MISSING"); err != nil {
					return err
				}
			}
		} else if err := recomputeSelection(ctx, txRepo, updatedFiles); err != nil {
			return err
		}

		finalFiles, err := txRepo.ListByProcess(ctx, processID)
		if err != nil {
			return err
		}
		if err := refreshUploadStatusAndScenario(ctx, tx, txRepo, s.process, processID, finalFiles, unmatchedStages); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return RegistryUploadResult{}, err
	}
	return result, nil
}

// recomputeSelection группирует файлы по (object_id, doc_stage, discipline, document_code) и
// прогоняет SelectCurrent на каждой группе (backend-plan.md §8.2).
func recomputeSelection(ctx context.Context, txRepo *Repository, allFiles []File) error {
	type groupKey struct{ objectID, stage, discipline, code string }
	groups := make(map[groupKey][]File)
	for _, f := range allFiles {
		if f.CheckStatus != CheckStatusAccepted {
			continue
		}
		k := groupKey{
			objectID: f.ObjectID.String(), stage: deref(f.DocStage), discipline: deref(f.Discipline), code: deref(f.DocumentCode),
		}
		groups[k] = append(groups[k], f)
	}

	for _, group := range groups {
		revFiles := make([]RevisionFile, 0, len(group))
		for _, f := range group {
			revFiles = append(revFiles, RevisionFile{
				ID: f.ID, PredecessorID: f.PredecessorID, SuccessorID: f.SuccessorID, ApprovalStatus: deref(f.ApprovalStatus),
			})
		}

		for _, outcome := range SelectCurrent(revFiles) {
			if err := txRepo.UpdateSelection(ctx, outcome.FileID, outcome.IsCurrent, outcome.Status, outcome.Reason); err != nil {
				return err
			}
		}
	}
	return nil
}

// refreshUploadStatusAndScenario — статус загрузки по стадии (§8.2, последний абзац) и сценарий
// (§8.4), пересчитываются после confirm/registry.
func refreshUploadStatusAndScenario(ctx context.Context, tx pgx.Tx, txRepo *Repository, gate ProcessGate, processID uuid.UUID, files []File, unmatchedStages map[string]bool) error {
	type stageInfo struct {
		hasFiles   bool
		hasCurrent bool
		hasClarify bool
	}
	stages := map[string]*stageInfo{process.StagePD: {}, process.StageRD: {}, process.StageID: {}}

	for _, f := range files {
		stage := deref(f.DocStage)
		info, ok := stages[stage]
		if !ok {
			continue
		}
		info.hasFiles = true
		if f.IsCurrent {
			info.hasCurrent = true
		}
		if f.SelectionStatus != nil && *f.SelectionStatus == SelectionStatusClarificationRequired {
			info.hasClarify = true
		}
	}

	var hasPD, hasRD, hasID, anyPartial bool
	for stage, info := range stages {
		var status string
		switch {
		case !info.hasFiles:
			status = process.UploadStatusMissing
		case info.hasCurrent && !info.hasClarify && !unmatchedStages[stage]:
			status = process.UploadStatusUploaded
		default:
			status = process.UploadStatusPartial
			anyPartial = true
		}

		if err := txRepo.UpsertUploadStatus(ctx, processID, stage, status); err != nil {
			return err
		}

		switch stage {
		case process.StagePD:
			hasPD = status != process.UploadStatusMissing
		case process.StageRD:
			hasRD = status != process.UploadStatusMissing
		case process.StageID:
			hasID = status != process.UploadStatusMissing
		}
	}

	scenario := process.ComputeScenario(hasPD, hasRD, hasID, anyPartial)
	return gate.UpdateScenario(ctx, tx, processID, scenario)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// RegistryTemplate — шаблон реестра: для процесса (предзаполненный его файлами) либо пустой (processID == nil).
func (s *Service) RegistryTemplate(ctx context.Context, processID *uuid.UUID, format string) (TemplateFile, error) {
	if processID == nil {
		return BuildRegistryTemplate(format, "", nil)
	}
	objectID, err := s.repo.ProcessObjectID(ctx, *processID)
	if err != nil {
		return TemplateFile{}, err
	}
	list, err := s.repo.ListByProcess(ctx, *processID)
	if err != nil {
		return TemplateFile{}, err
	}
	return BuildRegistryTemplate(format, objectID.String(), list)
}
