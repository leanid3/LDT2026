// Package files — загрузка, подтверждение, реестр и выбор актуальной редакции документов
// (backend-plan.md §8.1, §8.2).
package files

import "github.com/google/uuid"

// RevisionFile — вход алгоритма выбора редакции: одна запись внутри группы
// (object_id + doc_stage + discipline + document_code).
type RevisionFile struct {
	ID             uuid.UUID
	PredecessorID  *uuid.UUID
	SuccessorID    *uuid.UUID
	ApprovalStatus string // DRAFT | APPROVED | FOR_CONSTRUCTION | SUPERSEDED | CANCELLED
}

type SelectionOutcome struct {
	FileID    uuid.UUID
	IsCurrent bool
	Status    string // "" | CLARIFICATION_REQUIRED
	Reason    string
}

// Причины CLARIFICATION_REQUIRED — backend-plan.md §8.2, шаги 1-4.
const (
	SelectionStatusClarificationRequired = "CLARIFICATION_REQUIRED"

	SelectionReasonCycle              = "CYCLE"
	SelectionReasonDanglingReference  = "DANGLING_REFERENCE"
	SelectionReasonNoCandidate        = "NO_CANDIDATE"
	SelectionReasonMultipleCandidates = "MULTIPLE_CANDIDATES"
)

func isApprovable(status string) bool {
	return status == "APPROVED" || status == "FOR_CONSTRUCTION"
}

// SelectCurrent реализует алгоритм выбора актуальной редакции (backend-plan.md §8.2, шаги 1-4) для
// одной группы файлов:
//  1. Граф по predecessor/successor; цикл или ссылка на отсутствующую запись → вся группа
//     CLARIFICATION_REQUIRED.
//  2. SUPERSEDED/CANCELLED исключаются из кандидатов (остаются для аудита, is_current=false, без
//     CLARIFICATION_REQUIRED). DRAFT кандидатом быть не может.
//  3. Кандидаты — APPROVED/FOR_CONSTRUCTION без утверждённого потомка.
//  4. Ровно один кандидат → is_current=true. Ноль или 2+ → вся группа CLARIFICATION_REQUIRED.
func SelectCurrent(group []RevisionFile) []SelectionOutcome {
	byID := make(map[uuid.UUID]RevisionFile, len(group))
	for _, f := range group {
		byID[f.ID] = f
	}

	for _, f := range group {
		if f.PredecessorID != nil {
			if _, ok := byID[*f.PredecessorID]; !ok {
				return clarifyAll(group, SelectionReasonDanglingReference)
			}
		}
		if f.SuccessorID != nil {
			if _, ok := byID[*f.SuccessorID]; !ok {
				return clarifyAll(group, SelectionReasonDanglingReference)
			}
		}
	}

	if hasCycle(group, byID) {
		return clarifyAll(group, SelectionReasonCycle)
	}

	hasApprovedSuccessor := make(map[uuid.UUID]bool, len(group))
	for _, f := range group {
		if f.PredecessorID != nil && isApprovable(f.ApprovalStatus) {
			hasApprovedSuccessor[*f.PredecessorID] = true
		}
	}

	var candidates []uuid.UUID
	for _, f := range group {
		if isApprovable(f.ApprovalStatus) && !hasApprovedSuccessor[f.ID] {
			candidates = append(candidates, f.ID)
		}
	}

	switch len(candidates) {
	case 0:
		return clarifyAll(group, SelectionReasonNoCandidate)
	case 1:
		out := make([]SelectionOutcome, 0, len(group))
		for _, f := range group {
			out = append(out, SelectionOutcome{FileID: f.ID, IsCurrent: f.ID == candidates[0]})
		}
		return out
	default:
		return clarifyAll(group, SelectionReasonMultipleCandidates)
	}
}

func clarifyAll(group []RevisionFile, reason string) []SelectionOutcome {
	out := make([]SelectionOutcome, 0, len(group))
	for _, f := range group {
		out = append(out, SelectionOutcome{
			FileID: f.ID, IsCurrent: false, Status: SelectionStatusClarificationRequired, Reason: reason,
		})
	}
	return out
}

// hasCycle обходит цепочки successor (три цвета, DFS) — предыдущий проход graph.
func hasCycle(group []RevisionFile, byID map[uuid.UUID]RevisionFile) bool {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[uuid.UUID]int, len(group))

	var visit func(id uuid.UUID) bool
	visit = func(id uuid.UUID) bool {
		switch color[id] {
		case gray:
			return true
		case black:
			return false
		}
		color[id] = gray
		if f, ok := byID[id]; ok && f.SuccessorID != nil {
			if visit(*f.SuccessorID) {
				return true
			}
		}
		color[id] = black
		return false
	}

	for _, f := range group {
		if color[f.ID] == white {
			if visit(f.ID) {
				return true
			}
		}
	}
	return false
}
