package importer

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	dbm "github.com/cometbft/cometbft-db"
	gogotypes "github.com/cosmos/gogoproto/types"
	"github.com/terra-money/mantlemint/db/heleveldb"
	"github.com/terra-money/mantlemint/db/wrapped"
)

const progressEvery = 1_000_000

type Config struct {
	// AppHome is the terrad home whose data/application.db supplies module state.
	AppHome string
	// CometHome is the home of the node producing blocks after the import;
	// its data/state.db and data/blockstore.db are seeded. Defaults to AppHome.
	CometHome string
	// MantlemintHome and MantlemintDB locate the database to create,
	// matching mantlemint's MANTLEMINT_HOME and MANTLEMINT_DB.
	MantlemintHome string
	MantlemintDB   string
	// Height to import; 0 selects the latest committed version.
	Height     int64
	Workers    int
	FlushBytes int
	SkipWasm   bool
	Logf       func(format string, args ...any)
}

type StoreReport struct {
	Name     string
	Leaves   int64
	Duration time.Duration
}

type Report struct {
	Height  int64
	ChainID string
	Stores  []StoreReport
}

// ErrIncompleteImport marks failures after the target was created; the target
// database is left marked in-progress and must be deleted before retrying.
var ErrIncompleteImport = errors.New("import did not complete")

// Run imports state at a single height into a fresh mantlemint database.
func Run(cfg Config) (*Report, error) {
	if cfg.AppHome == "" || cfg.MantlemintHome == "" || cfg.MantlemintDB == "" {
		return nil, fmt.Errorf("app home, mantlemint home and mantlemint db are required")
	}
	if cfg.CometHome == "" {
		cfg.CometHome = cfg.AppHome
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}

	appState, err := OpenAppState(filepath.Join(cfg.AppHome, "data"), cfg.Height)
	if err != nil {
		return nil, err
	}
	defer appState.Close()

	comet, err := OpenCometState(filepath.Join(cfg.CometHome, "data"))
	if err != nil {
		return nil, err
	}
	defer comet.Close()

	height := appState.Height()
	if comet.Height() != height {
		return nil, fmt.Errorf("CometBFT state is at height %d but app state is at height %d; both sources must be at the same height",
			comet.Height(), height)
	}
	cfg.Logf("[import] height %d, chain id %s, %d stores", height, comet.ChainID(), len(appState.StoreNames()))

	target, err := OpenTarget(cfg.MantlemintHome, cfg.MantlemintDB)
	if err != nil {
		return nil, err
	}
	defer target.Close()

	report, err := importInto(cfg, target, appState, comet)
	if err != nil {
		return nil, fmt.Errorf("%w: %w; delete %s.db in %s before retrying",
			ErrIncompleteImport, err, cfg.MantlemintDB, cfg.MantlemintHome)
	}
	return report, nil
}

func importInto(cfg Config, target *Target, appState *AppStateSource, comet *CometSource) (*Report, error) {
	height := appState.Height()
	if err := target.Driver.SetImportState(heleveldb.ImportStateInProgress); err != nil {
		return nil, err
	}

	stores, err := importStores(cfg, target.Driver, appState)
	if err != nil {
		return nil, err
	}

	cfg.Logf("[import] seeding CometBFT state and commit metadata at height %d", height)
	err = target.WriteAt(height, func(db dbm.DB) error {
		if err := comet.SeedInto(wrapped.NewWrappedDB(db), height); err != nil {
			return err
		}
		latest, err := gogotypes.StdInt64Marshal(height)
		if err != nil {
			return err
		}
		if err := db.Set([]byte(latestVersionKey), latest); err != nil {
			return err
		}
		return db.Set([]byte(fmt.Sprintf(commitInfoKeyFmt, height)), appState.CommitInfoBytes())
	})
	if err != nil {
		return nil, err
	}

	if !cfg.SkipWasm {
		src := filepath.Join(cfg.AppHome, "data", "wasm")
		dst := filepath.Join(cfg.MantlemintHome, "data", "wasm")
		cfg.Logf("[import] copying %s to %s", src, dst)
		if err := copyWasmDir(src, dst); err != nil {
			return nil, err
		}
	}

	if err := target.Driver.SetImportFloor(height); err != nil {
		return nil, err
	}
	if err := target.Driver.SetImportState(heleveldb.ImportStateComplete); err != nil {
		return nil, err
	}

	return &Report{Height: height, ChainID: comet.ChainID(), Stores: stores}, nil
}

// importStores copies every store concurrently, then re-reads each store through
// the driver at the import height to prove the written entries are servable.
func importStores(cfg Config, driver *heleveldb.Driver, appState *AppStateSource) ([]StoreReport, error) {
	names := appState.StoreNames()
	jobs := make(chan string)
	results := make(chan StoreReport, len(names))

	var (
		failed   atomic.Bool
		errOnce  sync.Once
		firstErr error
		wg       sync.WaitGroup
	)
	fail := func(err error) {
		errOnce.Do(func() { firstErr = err })
		failed.Store(true)
	}

	for i := 0; i < cfg.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range jobs {
				if failed.Load() {
					continue
				}
				report, err := importStore(cfg, driver, appState, name, &failed)
				if err != nil {
					fail(err)
					continue
				}
				results <- report
			}
		}()
	}
	for _, name := range names {
		jobs <- name
	}
	close(jobs)
	wg.Wait()
	close(results)

	if firstErr != nil {
		return nil, firstErr
	}
	reports := make([]StoreReport, 0, len(names))
	for r := range results {
		reports = append(reports, r)
	}
	sort.Slice(reports, func(i, j int) bool { return reports[i].Name < reports[j].Name })
	return reports, nil
}

var errAborted = errors.New("aborted after another store failed")

func importStore(cfg Config, driver *heleveldb.Driver, appState *AppStateSource, name string, failed *atomic.Bool) (StoreReport, error) {
	height := appState.Height()
	start := time.Now()
	prefix := []byte(fmt.Sprintf(storeKeyPrefix, name))

	w, err := driver.NewBulkWriter(height, cfg.FlushBytes)
	if err != nil {
		return StoreReport{}, err
	}
	leaves, err := appState.IterateStore(name, func(key, value []byte) error {
		if failed.Load() {
			return errAborted
		}
		full := make([]byte, 0, len(prefix)+len(key))
		full = append(append(full, prefix...), key...)
		if err := w.Set(full, value); err != nil {
			return fmt.Errorf("store %q: write: %w", name, err)
		}
		if n := w.Count(); n%progressEvery == 0 {
			elapsed := time.Since(start)
			cfg.Logf("[import] %s: %d leaves, %.0f leaves/s", name, n, float64(n)/elapsed.Seconds())
		}
		return nil
	})
	closeErr := w.Close()
	if err != nil {
		return StoreReport{}, err
	}
	if closeErr != nil {
		return StoreReport{}, fmt.Errorf("store %q: flush: %w", name, closeErr)
	}

	if err := verifyStore(driver, name, height, leaves); err != nil {
		return StoreReport{}, err
	}

	elapsed := time.Since(start)
	cfg.Logf("[import] %s: done, %d leaves in %s", name, leaves, elapsed.Round(time.Millisecond))
	return StoreReport{Name: name, Leaves: leaves, Duration: elapsed}, nil
}

// verifyStore counts the store's keys the way an explicit-height query sees them:
// through the iterator key index, resolving each key's value at height.
func verifyStore(driver *heleveldb.Driver, name string, height, expected int64) error {
	start := []byte(fmt.Sprintf(storeKeyPrefix, name))
	it, err := driver.Iterator(height, start, prefixEnd(start))
	if err != nil {
		return fmt.Errorf("store %q: verify: %w", name, err)
	}
	defer it.Close()

	var count int64
	for ; it.Valid(); it.Next() {
		count++
	}
	if count != expected {
		return fmt.Errorf("store %q: wrote %d leaves but %d are readable at height %d", name, expected, count, height)
	}
	return nil
}

// prefixEnd returns the smallest key greater than every key with the given prefix.
func prefixEnd(prefix []byte) []byte {
	end := append([]byte{}, prefix...)
	for i := len(end) - 1; i >= 0; i-- {
		if end[i] < 0xff {
			end[i]++
			return end[:i+1]
		}
	}
	return nil
}

// copyWasmDir copies the wasm code directory. A missing source is an error,
// since contracts cannot run without their code; an existing destination is
// refused so blobs from another chain are never mixed in.
func copyWasmDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("wasm directory: %w (use the skip-wasm option to import without it)", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("wasm path %s is not a directory", src)
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("wasm destination %s already exists; remove it or use the skip-wasm option", dst)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}

		switch {
		case d.IsDir():
			return os.MkdirAll(out, info.Mode().Perm())
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, out)
		case info.Mode().IsRegular():
			return copyFile(path, out, info.Mode().Perm())
		default:
			return fmt.Errorf("wasm directory contains unsupported file %s", path)
		}
	})
}

func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
