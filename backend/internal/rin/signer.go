// Package rin — интеграция с ИАИС «РиН» (backend-plan.md §8.9): отправка финализированных протоколов
// с ретраями, синхронный HTTP-клиент внутри Kafka-consumer'а (допустимо благодаря
// max.poll.interval.ms ≥ 30 мин, backend-plan.md §6.3 — именно под такие долгие обработчики).
package rin

// Signer — УКЭП-подпись запросов в ИАИС «РиН». Реального крипто-провайдера нет и в ТЗ формат подписи
// не опубликован (backend-plan.md §12) — заглушка, как и предписано §8.9.
// TODO(TZ): формат подписи и провайдер УКЭП не определены в ТЗ — уточнить у заказчика.
type Signer interface {
	Sign(payload []byte) (signature string, err error)
}

type NoopSigner struct{}

func (NoopSigner) Sign([]byte) (string, error) { return "", nil }
