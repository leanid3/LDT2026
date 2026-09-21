// cmd/rin-mock — заглушка ИАИС «РиН» для демо: логирует запросы, по флагу отвечает 5xx для ретраев.
//
// TODO(M4): реализовать по backend-plan.md §8.9.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "rin-mock: not implemented yet, see backend-plan.md (M4)")
	os.Exit(1)
}
