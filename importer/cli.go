package importer

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"text/tabwriter"
	"time"

	"github.com/terra-money/mantlemint/db/heleveldb"
)

// CommandName is the mantlemint subcommand that runs an import.
const CommandName = "import"

// Main runs `mantlemint import` with the arguments following the subcommand
// and returns the process exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	var cfg Config
	fs := flag.NewFlagSet("mantlemint "+CommandName, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: mantlemint %s -app-home <terrad home> -mantlemint-home <mantlemint home> [flags]\n\n", CommandName)
		fmt.Fprintln(stderr, "Creates a mantlemint database from a stopped terrad node's data directory,")
		fmt.Fprintln(stderr, "so mantlemint starts at that height instead of genesis. State below the")
		fmt.Fprintln(stderr, "imported height is unavailable.")
		fmt.Fprintln(stderr)
		fs.PrintDefaults()
	}
	fs.StringVar(&cfg.AppHome, "app-home", "", "terrad home whose data/application.db supplies module state (required)")
	fs.StringVar(&cfg.CometHome, "comet-home", "", "home of the node that will produce blocks after the import; defaults to -app-home")
	fs.StringVar(&cfg.MantlemintHome, "mantlemint-home", "", "mantlemint home to import into, as MANTLEMINT_HOME (required)")
	fs.StringVar(&cfg.MantlemintDB, "mantlemint-db", "mantlemint", "mantlemint database name, as MANTLEMINT_DB")
	fs.Int64Var(&cfg.Height, "height", 0, "height to import; 0 imports the latest committed version")
	fs.IntVar(&cfg.Workers, "workers", 4, "number of stores imported concurrently; each store is read by one worker, so the largest store bounds the import time")
	fs.IntVar(&cfg.FlushBytes, "flush-bytes", heleveldb.DefaultBulkFlushBytes, "buffered bytes per store before writing to disk (at most 64MiB)")
	fs.BoolVar(&cfg.SkipWasm, "skip-wasm", false, "do not copy <app-home>/data/wasm")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected arguments: %v\n\n", fs.Args())
		fs.Usage()
		return 2
	}
	if cfg.AppHome == "" || cfg.MantlemintHome == "" {
		fmt.Fprintln(stderr, "-app-home and -mantlemint-home are required")
		fmt.Fprintln(stderr)
		fs.Usage()
		return 2
	}

	logger := log.New(stderr, "", log.LstdFlags)
	cfg.Logf = logger.Printf

	report, err := Run(cfg)
	if err != nil {
		if errors.Is(err, ErrIncompleteImport) {
			logger.Printf("[import] FAILED; mantlemint will refuse to start on this database")
		}
		logger.Printf("[import] %v", err)
		return 1
	}

	printReport(stdout, report)
	return 0
}

func printReport(w io.Writer, report *Report) {
	fmt.Fprintf(w, "\nimported chain %s at height %d\n\n", report.ChainID, report.Height)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "store\tleaves\tduration\t")
	var total int64
	for _, s := range report.Stores {
		fmt.Fprintf(tw, "%s\t%d\t%s\t\n", s.Name, s.Leaves, s.Duration.Round(time.Millisecond))
		total += s.Leaves
	}
	fmt.Fprintf(tw, "total\t%d\t\t\n", total)
	_ = tw.Flush()
	fmt.Fprintf(w, "\nstart mantlemint with CHAIN_ID=%s; queries below height %d return an error\n", report.ChainID, report.Height)
}
