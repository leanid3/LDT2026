// Package idempotency делает обработку одного Kafka-события идемпотентной через таблицу
// consumed_events (backend-plan.md §6.3, backend/CLAUDE.md правило 4): offset коммитится только
// после COMMIT транзакции Postgres, а повторная доставка того же event_id не выполняет бизнес-логику
// дважды.
package idempotency

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Tanoklllmonbku/Hakaton-LDT/backend/internal/platform/db"
)

// Once выполняет fn ровно один раз для пары (consumerName, eventID): BEGIN -> INSERT consumed_events
// ON CONFLICT DO NOTHING -> [fn, если 1 строка вставлена] -> COMMIT. Возвращает handled=false, если
// событие уже было обработано этим consumer'ом раньше (fn не вызывался).
func Once(ctx context.Context, database *db.DB, consumerName, eventID string, fn func(tx pgx.Tx) error) (handled bool, err error) {
	err = database.WithTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			INSERT INTO consumed_events (consumer_name, event_id)
			VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, consumerName, eventID)
		if err != nil {
			return fmt.Errorf("insert consumed_events: %w", err)
		}

		if tag.RowsAffected() == 0 {
			handled = false
			return nil
		}

		handled = true
		return fn(tx)
	})
	return handled, err
}
