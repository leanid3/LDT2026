// cmd/rin-sync — отправка результатов в ИАИС «РиН» после PROTOCOL_FINALIZED, ретраи 1/5/15 мин.
//
// TODO(M4): реализовать по backend-plan.md §8.9.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "rin-sync: not implemented yet, see backend-plan.md (M4)")
	os.Exit(1)
}
