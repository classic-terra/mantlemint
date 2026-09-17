package importer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	storetypes "cosmossdk.io/store/types"
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
	// Progress, if set, receives a snapshot of every store about once per
	// ProgressInterval while stores are imported, and replaces the per-store
	// log lines.
	Progress         func([]StoreProgress)
	ProgressInterval time.Duration
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
// Cancelling ctx stops the import; once the target exists it is left marked
// incomplete, as after any other failure.
func Run(ctx context.Context, cfg Config) (*Report, error) {
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

	// fail on anything checkable before the target is created; the store import can take hours
	height := appState.Height()
	if err := comet.Validate(height); err != nil {
		return nil, err
	}
	if cfg.FlushBytes > targetWriteBuffer/2 {
		return nil, fmt.Errorf("flush bytes %d exceeds the maximum of %d", cfg.FlushBytes, targetWriteBuffer/2)
	}
	if !cfg.SkipWasm {
		if _, err := checkWasmPaths(wasmSource(cfg), wasmDestination(cfg)); err != nil {
			return nil, err
		}
	}
	cfg.Logf("[import] height %d, chain id %s, %d stores", height, comet.ChainID(), len(appState.StoreNames()))

	// nothing is written yet, so stopping here leaves no target behind
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	target, err := OpenTarget(cfg.MantlemintHome, cfg.MantlemintDB)
	if err != nil {
		return nil, err
	}
	defer target.Close()

	report, err := importInto(ctx, cfg, target, appState, comet)
	if err != nil {
		leftovers := cfg.MantlemintDB + ".db"
		if !cfg.SkipWasm {
			leftovers += " and data/wasm"
		}
		return nil, fmt.Errorf("%w: %w; delete %s in %s before retrying",
			ErrIncompleteImport, err, leftovers, cfg.MantlemintHome)
	}
	return report, nil
}

func importInto(ctx context.Context, cfg Config, target *Target, appState *AppStateSource, comet *CometSource) (*Report, error) {
	height := appState.Height()
	if err := target.driver.SetImportState(heleveldb.ImportStateInProgress); err != nil {
		return nil, err
	}

	stores, err := importStores(ctx, cfg, target.driver, appState)
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
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
		cfg.Logf("[import] copying %s to %s", wasmSource(cfg), wasmDestination(cfg))
		if err := copyWasmDir(ctx, wasmSource(cfg), wasmDestination(cfg)); err != nil {
			return nil, err
		}
	}

	if err := target.driver.SetImportFloor(height); err != nil {
		return nil, err
	}
	if err := target.driver.SetImportState(heleveldb.ImportStateComplete); err != nil {
		return nil, err
	}

	return &Report{Height: height, ChainID: comet.ChainID(), Stores: stores}, nil
}

// importStores copies every store concurrently, then re-reads each store through
// the driver at the import height to prove the written entries are servable.
func importStores(ctx context.Context, cfg Config, driver *heleveldb.Driver, appState *AppStateSource) ([]StoreReport, error) {
	names := appState.StoreNames()
	// reading every root first costs seconds and gives the overall progress a fixed total
	totals := make([]int64, len(names))
	for i, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		size, err := appState.StoreSize(name)
		if err != nil {
			return nil, err
		}
		totals[i] = size
	}
	tracker := newProgressTracker(names, totals)
	if cfg.Progress != nil {
		if cfg.ProgressInterval <= 0 {
			cfg.ProgressInterval = time.Second
		}
		stop := tracker.report(cfg.Progress, cfg.ProgressInterval)
		defer stop()
	}
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
	// cancellation stops the workers the same way a failed store does
	storesDone := make(chan struct{})
	defer close(storesDone)
	go func() {
		select {
		case <-ctx.Done():
			fail(ctx.Err())
		case <-storesDone:
		}
	}()

	for i := 0; i < cfg.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range jobs {
				if failed.Load() {
					continue
				}
				report, err := importStore(cfg, driver, appState, tracker, name, &failed)
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
	errOnce.Do(func() {}) // no failure can be recorded past this point

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

// formatProgress reports done of total leaves with the average rate so far and
// the remaining time that rate implies.
func formatProgress(done, total int64, elapsed time.Duration) string {
	rate := float64(done) / elapsed.Seconds()
	if total <= 0 || done > total {
		return fmt.Sprintf("%d leaves, %.0f leaves/s", done, rate)
	}
	eta := time.Duration(float64(total-done) / rate * float64(time.Second))
	return fmt.Sprintf("%d/%d leaves (%.1f%%), %.0f leaves/s, ~%s left",
		done, total, 100*float64(done)/float64(total), rate, eta.Round(time.Second))
}

var errAborted = errors.New("aborted after another store failed or the import was interrupted")

func importStore(cfg Config, driver *heleveldb.Driver, appState *AppStateSource, tracker *progressTracker, name string, failed *atomic.Bool) (StoreReport, error) {
	height := appState.Height()
	start := time.Now()
	prefix := []byte(fmt.Sprintf(storeKeyPrefix, name))

	w, err := driver.NewBulkWriter(height, cfg.FlushBytes)
	if err != nil {
		return StoreReport{}, err
	}
	written := newLeafDigest()
	// the live display replaces per-store log lines
	logStore := cfg.Logf
	if cfg.Progress != nil {
		logStore = func(string, ...any) {}
	}
	total := tracker.started(name)
	if total >= progressEvery {
		logStore("[import] %s: %d leaves to import", name, total)
	}
	leaves, err := appState.IterateStore(name, func(key, value []byte) error {
		if failed.Load() {
			return errAborted
		}
		full := make([]byte, 0, len(prefix)+len(key))
		full = append(append(full, prefix...), key...)
		written.add(full, value)
		if err := w.Set(full, value); err != nil {
			return fmt.Errorf("store %q: write: %w", name, err)
		}
		n := w.Count()
		tracker.wrote(name, n)
		if n%progressEvery == 0 {
			logStore("[import] %s: %s", name, formatProgress(n, total, time.Since(start)))
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

	tracker.verifying(name)
	if err := verifyStore(driver, name, height, written, failed); err != nil {
		return StoreReport{}, err
	}
	tracker.finished(name)

	elapsed := time.Since(start)
	logStore("[import] %s: done, %d leaves in %s", name, leaves, elapsed.Round(time.Millisecond))
	return StoreReport{Name: name, Leaves: leaves, Duration: elapsed}, nil
}

// verifyStore reads the store back the way an explicit-height query does, through
// the iterator key index resolving each value at height, and requires the same
// keys and values, in the same order, as were written.
func verifyStore(driver *heleveldb.Driver, name string, height int64, written *leafDigest, failed *atomic.Bool) error {
	start := []byte(fmt.Sprintf(storeKeyPrefix, name))
	it, err := driver.Iterator(height, start, storetypes.PrefixEndBytes(start))
	if err != nil {
		return fmt.Errorf("store %q: verify: %w", name, err)
	}
	defer it.Close()

	read := newLeafDigest()
	for ; it.Valid(); it.Next() {
		if failed.Load() {
			return errAborted
		}
		read.add(it.Key(), it.Value())
	}
	if read.count != written.count {
		return fmt.Errorf("store %q: wrote %d leaves but %d are readable at height %d", name, written.count, read.count, height)
	}
	if !bytes.Equal(read.sum(), written.sum()) {
		return fmt.Errorf("store %q: leaves readable at height %d differ from the leaves written", name, height)
	}
	return nil
}

// leafDigest hashes an ordered sequence of key/value pairs.
type leafDigest struct {
	hash  hash.Hash
	count int64
	lens  [8]byte
}

func newLeafDigest() *leafDigest {
	return &leafDigest{hash: sha256.New()}
}

func (d *leafDigest) add(key, value []byte) {
	// length-prefix both parts so key/value boundaries cannot shift between entries
	binary.BigEndian.PutUint32(d.lens[:4], uint32(len(key)))
	binary.BigEndian.PutUint32(d.lens[4:], uint32(len(value)))
	d.hash.Write(d.lens[:])
	d.hash.Write(key)
	d.hash.Write(value)
	d.count++
}

func (d *leafDigest) sum() []byte {
	return d.hash.Sum(nil)
}

func wasmSource(cfg Config) string {
	return filepath.Join(cfg.AppHome, "data", "wasm")
}

func wasmDestination(cfg Config) string {
	return filepath.Join(cfg.MantlemintHome, "data", "wasm")
}

// checkWasmPaths resolves the source wasm directory through symlinks and
// requires the destination to be absent, so blobs from another chain are never
// mixed in.
func checkWasmPaths(src, dst string) (string, error) {
	resolved, err := filepath.EvalSymlinks(src)
	if err != nil {
		return "", fmt.Errorf("wasm directory: %w (use the skip-wasm option to import without it)", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("wasm directory: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("wasm path %s is not a directory", src)
	}
	if _, err := os.Lstat(dst); err == nil {
		return "", fmt.Errorf("wasm destination %s already exists; remove it or use the skip-wasm option", dst)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	return resolved, nil
}

// copyWasmDir copies the wasm code directory. A symlinked source root is
// followed; symlinks inside it are refused, since recreating them would point
// the copy back into the source node's files.
func copyWasmDir(ctx context.Context, src, dst string) error {
	resolved, err := checkWasmPaths(src, dst)
	if err != nil {
		return err
	}

	return filepath.WalkDir(resolved, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(resolved, path)
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
		case info.Mode().IsRegular():
			return copyFile(path, out, info.Mode().Perm())
		case info.Mode()&fs.ModeSymlink != 0:
			return fmt.Errorf("wasm directory contains symlink %s; copy it manually and use the skip-wasm option", path)
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
	// the complete marker is written after the copy; make the copy durable first
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
