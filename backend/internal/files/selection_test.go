package files_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/files"
)

// helper: создаёт n файлов с последовательными uuid, чтобы тесты были читаемы по имени/индексу.
func ids(n int) []uuid.UUID {
	out := make([]uuid.UUID, n)
	for i := range out {
		out[i] = uuid.New()
	}
	return out
}

func ptr(id uuid.UUID) *uuid.UUID { return &id }

func outcomeFor(t *testing.T, outcomes []files.SelectionOutcome, id uuid.UUID) files.SelectionOutcome {
	t.Helper()
	for _, o := range outcomes {
		if o.FileID == id {
			return o
		}
	}
	t.Fatalf("no outcome for file %s", id)
	return files.SelectionOutcome{}
}

func TestSelectCurrent_SingleApprovedChain(t *testing.T) {
	// A (SUPERSEDED) -> B (SUPERSEDED) -> C (FOR_CONSTRUCTION, актуальна).
	id := ids(3)
	a, b, c := id[0], id[1], id[2]
	group := []files.RevisionFile{
		{ID: a, SuccessorID: ptr(b), ApprovalStatus: "SUPERSEDED"},
		{ID: b, PredecessorID: ptr(a), SuccessorID: ptr(c), ApprovalStatus: "SUPERSEDED"},
		{ID: c, PredecessorID: ptr(b), ApprovalStatus: "FOR_CONSTRUCTION"},
	}

	out := files.SelectCurrent(group)

	require.True(t, outcomeFor(t, out, c).IsCurrent)
	require.False(t, outcomeFor(t, out, a).IsCurrent)
	require.False(t, outcomeFor(t, out, b).IsCurrent)
	require.Empty(t, outcomeFor(t, out, a).Status, "SUPERSEDED не должен давать CLARIFICATION_REQUIRED")
}

func TestSelectCurrent_SingleFile_Approved(t *testing.T) {
	id := ids(1)[0]
	out := files.SelectCurrent([]files.RevisionFile{{ID: id, ApprovalStatus: "APPROVED"}})

	require.True(t, outcomeFor(t, out, id).IsCurrent)
}

func TestSelectCurrent_Cycle(t *testing.T) {
	id := ids(2)
	a, b := id[0], id[1]
	group := []files.RevisionFile{
		{ID: a, SuccessorID: ptr(b), ApprovalStatus: "APPROVED"},
		{ID: b, SuccessorID: ptr(a), ApprovalStatus: "APPROVED"}, // цикл A -> B -> A
	}

	out := files.SelectCurrent(group)

	require.Equal(t, files.SelectionReasonCycle, outcomeFor(t, out, a).Reason)
	require.False(t, outcomeFor(t, out, a).IsCurrent)
	require.False(t, outcomeFor(t, out, b).IsCurrent)
}

func TestSelectCurrent_DanglingSuccessor(t *testing.T) {
	id := ids(1)
	ghost := uuid.New()
	group := []files.RevisionFile{
		{ID: id[0], SuccessorID: &ghost, ApprovalStatus: "APPROVED"},
	}

	out := files.SelectCurrent(group)
	require.Equal(t, files.SelectionReasonDanglingReference, outcomeFor(t, out, id[0]).Reason)
}

func TestSelectCurrent_DanglingPredecessor(t *testing.T) {
	id := ids(1)
	ghost := uuid.New()
	group := []files.RevisionFile{
		{ID: id[0], PredecessorID: &ghost, ApprovalStatus: "APPROVED"},
	}

	out := files.SelectCurrent(group)
	require.Equal(t, files.SelectionReasonDanglingReference, outcomeFor(t, out, id[0]).Reason)
}

func TestSelectCurrent_NoCandidate_AllSuperseded(t *testing.T) {
	id := ids(2)
	group := []files.RevisionFile{
		{ID: id[0], ApprovalStatus: "SUPERSEDED"},
		{ID: id[1], ApprovalStatus: "CANCELLED"},
	}

	out := files.SelectCurrent(group)
	require.Equal(t, files.SelectionReasonNoCandidate, outcomeFor(t, out, id[0]).Reason)
	require.Equal(t, files.SelectionReasonNoCandidate, outcomeFor(t, out, id[1]).Reason)
}

func TestSelectCurrent_NoCandidate_OnlyDraft(t *testing.T) {
	id := ids(1)
	group := []files.RevisionFile{{ID: id[0], ApprovalStatus: "DRAFT"}}

	out := files.SelectCurrent(group)
	require.Equal(t, files.SelectionReasonNoCandidate, outcomeFor(t, out, id[0]).Reason)
	require.False(t, outcomeFor(t, out, id[0]).IsCurrent, "DRAFT не может быть эталоном")
}

func TestSelectCurrent_MultipleCandidates(t *testing.T) {
	id := ids(2)
	group := []files.RevisionFile{
		{ID: id[0], ApprovalStatus: "APPROVED"},
		{ID: id[1], ApprovalStatus: "FOR_CONSTRUCTION"}, // независимая ветка, не связаны
	}

	out := files.SelectCurrent(group)
	require.Equal(t, files.SelectionReasonMultipleCandidates, outcomeFor(t, out, id[0]).Reason)
	require.Equal(t, files.SelectionReasonMultipleCandidates, outcomeFor(t, out, id[1]).Reason)
}

func TestSelectCurrent_EmptyGroup(t *testing.T) {
	out := files.SelectCurrent(nil)
	require.Empty(t, out)
}
