// mantlemint-import creates a mantlemint database from a stopped terrad node's
// data directory, so mantlemint can start at that height instead of genesis.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"text/tabwriter"

	"github.com/terra-money/mantlemint/db/heleveldb"
	"github.com/terra-money/mantlemint/importer"
)

func main() {
	var cfg importer.Config
	flag.StringVar(&cfg.AppHome, "app-home", "", "terrad home whose data/application.db supplies module state (required)")
	flag.StringVar(&cfg.CometHome, "comet-home", "", "home of the node that will produce blocks after the import; defaults to -app-home")
	flag.StringVar(&cfg.MantlemintHome, "mantlemint-home", "", "mantlemint home to import into, as MANTLEMINT_HOME (required)")
	flag.StringVar(&cfg.MantlemintDB, "mantlemint-db", "mantlemint", "mantlemint database name, as MANTLEMINT_DB")
	flag.Int64Var(&cfg.Height, "height", 0, "height to import; 0 imports the latest committed version")
	flag.IntVar(&cfg.Workers, "workers", 4, "number of stores imported concurrently")
	flag.IntVar(&cfg.FlushBytes, "flush-bytes", heleveldb.DefaultBulkFlushBytes, "buffered bytes per store before writing to disk")
	flag.BoolVar(&cfg.SkipWasm, "skip-wasm", false, "do not copy <app-home>/data/wasm")
	flag.Parse()

	if cfg.AppHome == "" || cfg.MantlemintHome == "" {
		flag.Usage()
		os.Exit(2)
	}
	cfg.Logf = log.Printf

	report, err := importer.Run(cfg)
	if err != nil {
		if errors.Is(err, importer.ErrIncompleteImport) {
			log.Printf("[import] FAILED; mantlemint will refuse to start on this database")
		}
		log.Fatalf("[import] %v", err)
	}

	fmt.Printf("\nimported chain %s at height %d\n\n", report.ChainID, report.Height)
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "store\tleaves\tduration\t")
	var total int64
	for _, s := range report.Stores {
		fmt.Fprintf(tw, "%s\t%d\t%s\t\n", s.Name, s.Leaves, s.Duration.Round(1e6))
		total += s.Leaves
	}
	fmt.Fprintf(tw, "total\t%d\t\t\n", total)
	_ = tw.Flush()
	fmt.Printf("\nstart mantlemint with CHAIN_ID=%s; queries below height %d return an error\n", report.ChainID, report.Height)
}
