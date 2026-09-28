// cmd/rin-mock — заглушка ИАИС «РиН» для демо: логирует запросы, флаг --fail-every=N отвечает 5xx на
// каждый N-й запрос для демонстрации ретраев cmd/rin-sync (backend-plan.md §8.9).
package main

import (
	"flag"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync/atomic"
)

func main() {
	addr := flag.String("addr", ":8082", "адрес HTTP-сервера")
	failEvery := flag.Int("fail-every", 0, "отвечать 503 на каждый N-й запрос (0 — никогда не отвечать ошибкой)")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "rin-mock")

	var counter int64
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		defer func() { _ = r.Body.Close() }()

		n := atomic.AddInt64(&counter, 1)
		if *failEvery > 0 && n%int64(*failEvery) == 0 {
			log.Warn("simulated failure", "request_number", n, "body_bytes", len(body))
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		log.Info("received protocol sync", "request_number", n, "body", string(body))
		w.WriteHeader(http.StatusOK)
	})

	log.Info("rin-mock service started", "addr", *addr, "fail_every", *failEvery)
	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Error("server failed", "error", err)
		os.Exit(1)
	}
}
