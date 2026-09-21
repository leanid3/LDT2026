// cmd/tools/import-matrix — импорт листа МАТРИЦА (132 параметра) из docs/source/*.xlsx в таблицу params.
//
// TODO(M2): реализовать по backend-plan.md §5 (params), §11 (M2 DoD: make seed грузит 132 параметра).
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "import-matrix: not implemented yet, see backend-plan.md (M2)")
	os.Exit(1)
}
