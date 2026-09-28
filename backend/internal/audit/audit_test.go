package audit_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/audit"
	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/dbtest"
)

func TestLog_WritesRowWithinTx(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()

	userID := uuid.New()
	objectID := uuid.New()

	_, err := database.Pool().Exec(ctx, `
		INSERT INTO users (id, login, password_hash, full_name, role) VALUES ($1, 'auditor', 'x', 'Аудитор', 'admin')
	`, userID)
	require.NoError(t, err)

	err = database.WithTx(ctx, func(tx pgx.Tx) error {
		return audit.Log(ctx, tx, audit.Entry{
			UserID:     userID,
			Action:     "finding.decide",
			ObjectType: "finding",
			ObjectID:   objectID.String(),
			Details:    map[string]string{"decision": "CONFIRMED_VIOLATION"},
			Before:     map[string]string{"inspector_status": "PENDING"},
			After:      map[string]string{"inspector_status": "CONFIRMED_VIOLATION"},
			RequestID:  "req-1",
		})
	})
	require.NoError(t, err)

	var action, objectType, objType2, requestID string
	var details []byte
	row := database.Pool().QueryRow(ctx,
		`SELECT action, object_type, object_id, details, request_id FROM audit_log WHERE object_id = $1`,
		objectID.String())
	require.NoError(t, row.Scan(&action, &objectType, &objType2, &details, &requestID))

	require.Equal(t, "finding.decide", action)
	require.Equal(t, "finding", objectType)
	require.Equal(t, "req-1", requestID)

	var d map[string]string
	require.NoError(t, json.Unmarshal(details, &d))
	require.Equal(t, "CONFIRMED_VIOLATION", d["decision"])
}

func TestLog_RollsBackWithBusinessChangeOnError(t *testing.T) {
	database := dbtest.NewPostgres(t)
	ctx := context.Background()

	objID := uuid.New()
	err := database.WithTx(ctx, func(tx pgx.Tx) error {
		if err := audit.Log(ctx, tx, audit.Entry{Action: "noop", ObjectType: "t", ObjectID: objID.String()}); err != nil {
			return err
		}
		return errIntentional
	})
	require.ErrorIs(t, err, errIntentional)

	var count int
	require.NoError(t, database.Pool().QueryRow(ctx,
		`SELECT count(*) FROM audit_log WHERE object_id = $1`, objID.String(),
	).Scan(&count))
	require.Zero(t, count, "audit-запись должна откатиться вместе с бизнес-изменением (unit of work)")
}

var errIntentional = intentionalError{}

type intentionalError struct{}

func (intentionalError) Error() string { return "intentional rollback" }
