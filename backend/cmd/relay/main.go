// cmd/relay — outbox → Kafka: публикует события из outbox_events с ожиданием delivery report.
//
// TODO(M1): реализовать по backend-plan.md §6.4 (platform/outbox, claim FOR UPDATE SKIP LOCKED).
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "relay: not implemented yet, see backend-plan.md (M1)")
	os.Exit(1)
}
