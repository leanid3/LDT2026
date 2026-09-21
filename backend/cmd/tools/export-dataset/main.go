// cmd/tools/export-dataset — экспорт GOLD-датасета (CONFIRMED_VIOLATION/NEGATIVE_VERIFIED) для ML.
//
// TODO(M4): реализовать по backend-plan.md §8.4, §9.4.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "export-dataset: not implemented yet, see backend-plan.md (M4)")
	os.Exit(1)
}
