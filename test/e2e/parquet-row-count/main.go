// parquet-row-count — counts rows in a Parquet file and prints the count to stdout.
//
// Purpose:
//   Tiny CLI utility used by E2E test assertions to verify that a Parquet file
//   produced by radstorm contains the expected number of rows. Cleaner and more
//   portable than shelling out to Python for pyarrow. Reads the Parquet footer
//   only (does not decode column data) so it is fast even for large files.
//
// Related files:
//   - test/e2e/assertions.sh (calls this binary via assert_parquet_rows)
//   - test/e2e/scenarios/smoke-100.sh (uses it to check subscribers.parquet)
//   - pkg/collector/ (produces the Parquet files being validated)
//   - go.mod (module + parquet-go dependency)
//
// Briefing: .orchestration/briefings/3d-e2e.md
//
// Contract: reads one argument (path to .parquet file), prints row count as a
//           plain decimal integer to stdout, exits 0. Non-zero exit on error.
package main

import (
	"fmt"
	"os"

	goparquet "github.com/parquet-go/parquet-go"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: parquet-row-count <file.parquet>\n")
		os.Exit(1)
	}

	path := os.Args[1]

	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open %s: %v\n", path, err)
		os.Exit(1)
	}
	defer f.Close() //nolint:errcheck

	fi, err := f.Stat()
	if err != nil {
		fmt.Fprintf(os.Stderr, "stat %s: %v\n", path, err)
		os.Exit(1)
	}

	pf, err := goparquet.OpenFile(f, fi.Size())
	if err != nil {
		fmt.Fprintf(os.Stderr, "open parquet %s: %v\n", path, err)
		os.Exit(1)
	}

	fmt.Println(pf.NumRows())
}
