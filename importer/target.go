package importer

import (
	"fmt"
	"os"

	dbm "github.com/cometbft/cometbft-db"
	"github.com/terra-money/mantlemint/db/heleveldb"
	"github.com/terra-money/mantlemint/db/hld"
	"github.com/terra-money/mantlemint/db/safe_batch"
)

// Target is a mantlemint database assembled the same way sync.go assembles it:
// heleveldb driver, height-limited DB, then a single safe batch.
type Target struct {
	driver  *heleveldb.Driver
	hldb    *hld.HeightLimitedDB
	batched safe_batch.SafeBatchDBCloser
}

// OpenTarget opens <dir>/<name>.db. It refuses a database that already holds data.
func OpenTarget(dir, name string) (*Target, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	driver, err := heleveldb.NewLevelDBDriver(&heleveldb.DriverConfig{
		Name: name,
		Dir:  dir,
		Mode: heleveldb.DriverModeKeySuffixDesc,
	})
	if err != nil {
		return nil, fmt.Errorf("open target %s.db in %s: %w", name, dir, err)
	}

	empty, err := isEmpty(driver)
	if err != nil {
		_ = driver.Close()
		return nil, err
	}
	if !empty {
		_ = driver.Close()
		return nil, fmt.Errorf("target %s.db in %s is not empty; import only into a fresh database", name, dir)
	}

	hldb := hld.ApplyHeightLimitedDB(driver, &hld.HeightLimitedDBConfig{Debug: false})
	return &Target{
		driver:  driver,
		hldb:    hldb,
		batched: safe_batch.NewSafeBatchDB(hldb).(safe_batch.SafeBatchDBCloser),
	}, nil
}

func isEmpty(driver *heleveldb.Driver) (bool, error) {
	it, err := driver.Iterator(0, nil, nil)
	if err != nil {
		return false, err
	}
	defer it.Close()
	if it.Valid() {
		return false, nil
	}
	state, err := driver.ImportState()
	if err != nil {
		return false, err
	}
	return state == heleveldb.ImportStateNone, nil
}

// WriteAt runs fn against the batched database with writes recorded at height,
// then flushes. Writes made by fn are discarded if it returns an error.
func (t *Target) WriteAt(height int64, fn func(db dbm.DB) error) error {
	t.hldb.SetWriteHeight(height)
	defer t.hldb.ClearWriteHeight()

	t.batched.Open()
	if err := fn(t.batched); err != nil {
		// the batch is never flushed; the next Open replaces it
		return err
	}
	rollback, err := t.batched.Flush()
	if rollback != nil {
		_ = rollback.Close()
	}
	return err
}

func (t *Target) Close() error {
	return t.driver.Close()
}
