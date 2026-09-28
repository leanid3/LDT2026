package engine_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	miniogo "github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/require"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/engine"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/engine/rules"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/dbtest"
)

func TestValidateFact(t *testing.T) {
	ok := engine.Fact{Stage: "PD", ParamCode: "M-001", Quote: "цитата", Page: 3, BBox: [4]float64{0.1, 0.2, 0.3, 0.4}}
	require.NoError(t, engine.ValidateFact(ok))

	unlocalized := ok
	unlocalized.BBox = [4]float64{}
	require.NoError(t, engine.ValidateFact(unlocalized), "нулевой bbox = положение неизвестно, факт допустим")

	for name, mutate := range map[string]func(*engine.Fact){
		"нет quote":         func(f *engine.Fact) { f.Quote = "  " },
		"плохая стадия":     func(f *engine.Fact) { f.Stage = "XX" },
		"нет param_code":    func(f *engine.Fact) { f.ParamCode = "" },
		"bbox > 1":          func(f *engine.Fact) { f.BBox = [4]float64{0.1, 0.2, 1.3, 0.4} },
		"bbox < 0":          func(f *engine.Fact) { f.BBox = [4]float64{-0.1, 0.2, 0.3, 0.4} },
		"x0 >= x1":          func(f *engine.Fact) { f.BBox = [4]float64{0.5, 0.2, 0.5, 0.4} },
		"y0 >= y1":          func(f *engine.Fact) { f.BBox = [4]float64{0.1, 0.6, 0.3, 0.4} },
		"отрицательная стр": func(f *engine.Fact) { f.Page = -1 },
	} {
		bad := ok
		mutate(&bad)
		require.Error(t, engine.ValidateFact(bad), name)
	}
}

func TestFact_Comparable(t *testing.T) {
	require.Equal(t, "1250.5", engine.Fact{ValueRaw: "1 250,5 м²", ValueNorm: json.RawMessage(`{"kind":"number","value":1250.5}`)}.Comparable())
	require.Equal(t, "B30", engine.Fact{ValueRaw: "Бетон В30 W6", ValueNorm: json.RawMessage(`{"kind":"ordinal","value":"B30"}`)}.Comparable())
	require.Equal(t, "сырое", engine.Fact{ValueRaw: "сырое"}.Comparable(), "без value_norm — value_raw")
	require.Equal(t, "сырое", engine.Fact{ValueRaw: "сырое", ValueNorm: json.RawMessage(`{broken`)}.Comparable(), "битый value_norm не роняет разбор")
}

func TestHandler_ExtractCompleted_EmptyKeyErrorAndInvalidFacts(t *testing.T) {
	database := dbtest.NewPostgres(t)
	minioClient := dbtest.NewMinio(t)
	ctx := context.Background()

	newProcess := func() (objectID, processID, fileID, jobID uuid.UUID) {
		objectID, processID, fileID, jobID = uuid.New(), uuid.New(), uuid.New(), uuid.New()
		_, err := database.Pool().Exec(ctx, `INSERT INTO objects (id, name) VALUES ($1, 'Объект')`, objectID)
		require.NoError(t, err)
		_, err = database.Pool().Exec(ctx, `INSERT INTO processes (id, object_id, status) VALUES ($1, $2, 'PARSING')`, processID, objectID)
		require.NoError(t, err)
		_, err = database.Pool().Exec(ctx, `
			INSERT INTO files (id, object_id, process_id, original_name, storage_key, size_bytes, check_status, is_current)
			VALUES ($1, $2, $3, 'a.pdf', 'k', 1, 'ACCEPTED', true)`, fileID, objectID, processID)
		require.NoError(t, err)
		_, err = database.Pool().Exec(ctx, `INSERT INTO extract_jobs (id, process_id, status) VALUES ($1, $2, 'PENDING')`, jobID, processID)
		require.NoError(t, err)
		return
	}
	h := engine.NewHandler(database, minioClient, map[string]rules.Rule{"M-001": {Code: "M-001", Type: rules.TypeNumericEqual}})

	t.Run("пустой facts_key и error -> job FAILED, процесс не зависает", func(t *testing.T) {
		objectID, processID, _, jobID := newProcess()
		require.NoError(t, h.Handle(ctx, kafkaMessage(t, "doc.extract.completed", map[string]any{
			"event_id": uuid.NewString(), "event_type": "doc.extract.completed",
			"object_id": objectID.String(), "process_id": processID.String(),
			"data": map[string]any{"job_id": jobID.String(), "facts_key": "", "error": "llm недоступен"},
		})))

		var jobStatus, procStatus string
		require.NoError(t, database.Pool().QueryRow(ctx, `SELECT status FROM extract_jobs WHERE id = $1`, jobID).Scan(&jobStatus))
		require.NoError(t, database.Pool().QueryRow(ctx, `SELECT status FROM processes WHERE id = $1`, processID).Scan(&procStatus))
		require.Equal(t, "FAILED", jobStatus)
		require.Equal(t, "READY", procStatus, "FAILED — терминальный статус, процесс идёт дальше (ТЗ 9.1)")
	})

	t.Run("пустой facts_key без ошибки -> DONE без фактов", func(t *testing.T) {
		objectID, processID, _, jobID := newProcess()
		require.NoError(t, h.Handle(ctx, kafkaMessage(t, "doc.extract.completed", map[string]any{
			"event_id": uuid.NewString(), "event_type": "doc.extract.completed",
			"object_id": objectID.String(), "process_id": processID.String(),
			"data": map[string]any{"job_id": jobID.String(), "facts_key": ""},
		})))
		var jobStatus string
		require.NoError(t, database.Pool().QueryRow(ctx, `SELECT status FROM extract_jobs WHERE id = $1`, jobID).Scan(&jobStatus))
		require.Equal(t, "DONE", jobStatus)
	})

	t.Run("невалидные факты отбрасываются, валидные сохраняются", func(t *testing.T) {
		objectID, processID, fileID, jobID := newProcess()
		key := "facts/" + jobID.String() + ".jsonl"
		var buf bytes.Buffer
		for _, f := range []map[string]any{
			{"fact_id": uuid.NewString(), "file_id": fileID.String(), "stage": "PD", "param_code": "M-001", "value_raw": "100", "quote": "ок", "bbox": []float64{0.1, 0.1, 0.2, 0.2}},
			{"fact_id": uuid.NewString(), "file_id": fileID.String(), "stage": "RD", "param_code": "M-001", "value_raw": "90", "quote": "", "bbox": []float64{0.1, 0.1, 0.2, 0.2}},  // без quote
			{"fact_id": uuid.NewString(), "file_id": fileID.String(), "stage": "RD", "param_code": "M-001", "value_raw": "95", "quote": "x", "bbox": []float64{0.1, 0.1, 1.7, 0.2}}, // bbox вне [0;1]
		} {
			line, _ := json.Marshal(f)
			buf.Write(line)
			buf.WriteByte('\n')
		}
		_, err := minioClient.Raw().PutObject(ctx, minioClient.BucketName(), key, &buf, int64(buf.Len()), miniogo.PutObjectOptions{})
		require.NoError(t, err)

		require.NoError(t, h.Handle(ctx, kafkaMessage(t, "doc.extract.completed", map[string]any{
			"event_id": uuid.NewString(), "event_type": "doc.extract.completed",
			"object_id": objectID.String(), "process_id": processID.String(),
			"data": map[string]any{"job_id": jobID.String(), "facts_key": key},
		})))

		var facts int
		require.NoError(t, database.Pool().QueryRow(ctx, `SELECT count(*) FROM facts WHERE process_id = $1`, processID).Scan(&facts))
		require.Equal(t, 1, facts, "остался только валидный факт")

		var status string
		require.NoError(t, database.Pool().QueryRow(ctx, `SELECT finding_status FROM checks WHERE process_id = $1`, processID).Scan(&status))
		require.Equal(t, "MISSING_EVIDENCE", status, "РД-факта нет (отброшен) -> нечем сравнить")
	})
}
