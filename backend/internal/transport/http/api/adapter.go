package api

import (
	"context"

	"github.com/google/uuid"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/files"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/process"
)

// fileListerAdapter — единственное место, конвертирующее files.File в process.CurrentFile: process
// не может импортировать files (цикл, см. server.go), поэтому сам объявляет узкий интерфейс
// FileLister, а этот адаптер сводит два независимых доменных типа друг к другу.
type fileListerAdapter struct {
	repo *files.Repository
}

// NewFileLister — process.FileLister поверх files.Repository, для process.NewService.
func NewFileLister(repo *files.Repository) process.FileLister {
	return fileListerAdapter{repo: repo}
}

func (a fileListerAdapter) ListCurrentAccepted(ctx context.Context, processID uuid.UUID) ([]process.CurrentFile, error) {
	fs, err := a.repo.ListCurrentAccepted(ctx, processID)
	if err != nil {
		return nil, err
	}

	out := make([]process.CurrentFile, 0, len(fs))
	for _, f := range fs {
		cf := process.CurrentFile{ID: f.ID, StorageKey: f.StorageKey}
		if f.DocStage != nil {
			cf.DocStage = *f.DocStage
		}
		if f.Discipline != nil {
			cf.Discipline = *f.Discipline
		}
		if f.SHA256 != nil {
			cf.SHA256 = *f.SHA256
		}
		if f.ContentType != nil {
			cf.ContentType = *f.ContentType
			cf.IsPDF = *f.ContentType == "application/pdf"
		}
		if f.PageCount != nil {
			cf.PageCount = *f.PageCount
		}
		out = append(out, cf)
	}
	return out, nil
}
