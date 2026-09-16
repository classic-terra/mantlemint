package importer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	storetypes "cosmossdk.io/store/types"
	cmtlog "github.com/cometbft/cometbft/libs/log"
	tendermint "github.com/cometbft/cometbft/types"
	gogotypes "github.com/cosmos/gogoproto/types"
	"github.com/stretchr/testify/assert"
	"github.com/terra-money/mantlemint/db/heleveldb"
	"github.com/terra-money/mantlemint/db/hld"
	"github.com/terra-money/mantlemint/db/safe_batch"
	"github.com/terra-money/mantlemint/mantlemint"
	"github.com/terra-money/mantlemint/store/rootmulti"
)

var fixtureStores = []string{"bank", "empty", "wasm"}

// newSourceHome builds a terrad home with app state and CometBFT state at cometHeight
// and a small wasm directory.
func newSourceHome(t *testing.T, cometHeight int64) string {
	t.Helper()
	home := t.TempDir()
	data := filepath.Join(home, "data")
	assert.Nil(t, os.MkdirAll(data, 0o755))

	buildAppDB(t, data, fixtureStores, [][]fixtureOp{
		{set("bank", "a", "a1"), set("bank", "b", "b1"), set("wasm", "code", "wasm-bytes")},
		{set("bank", "b", "b2"), set("bank", "c", "c2")},
		{set("bank", "b", "b3"), del("bank", "a")},
	})
	buildCometDB(t, data, "rehearsal-1", cometHeight)

	wasmState := filepath.Join(data, "wasm", "wasm", "state", "wasm")
	assert.Nil(t, os.MkdirAll(wasmState, 0o755))
	assert.Nil(t, os.WriteFile(filepath.Join(wasmState, "checksum-1"), []byte("\x00asm contract one"), 0o644))
	assert.Nil(t, os.WriteFile(filepath.Join(data, "wasm", "wasm", "filesystem.lock"), nil, 0o644))
	return home
}

// openImported opens an imported database the way sync.go does.
type importedDB struct {
	driver  *heleveldb.Driver
	hldb    *hld.HeightLimitedDB
	batched safe_batch.SafeBatchDBCloser
}

func openImported(t *testing.T, home string) *importedDB {
	t.Helper()
	driver, err := heleveldb.NewLevelDBDriver(&heleveldb.DriverConfig{
		Name: "mantlemint",
		Dir:  home,
		Mode: heleveldb.DriverModeKeySuffixDesc,
	})
	assert.Nil(t, err)
	t.Cleanup(func() { _ = driver.Close() })
	hldb := hld.ApplyHeightLimitedDB(driver, &hld.HeightLimitedDBConfig{})
	return &importedDB{
		driver:  driver,
		hldb:    hldb,
		batched: safe_batch.NewSafeBatchDB(hldb).(safe_batch.SafeBatchDBCloser),
	}
}

func (db *importedDB) loadStore(t *testing.T) (*rootmulti.Store, map[string]storetypes.StoreKey) {
	t.Helper()
	rs := rootmulti.NewStore(db.batched, cmtlog.NewNopLogger(), db.hldb)
	keys := map[string]storetypes.StoreKey{}
	for _, name := range fixtureStores {
		keys[name] = storetypes.NewKVStoreKey(name)
		rs.MountStoreWithDB(keys[name], storetypes.StoreTypeDB, nil)
	}
	assert.Nil(t, rs.LoadLatestVersion())
	return rs, keys
}

func storeContents(t *testing.T, store storetypes.KVStore) map[string]string {
	t.Helper()
	it := store.Iterator(nil, nil)
	defer it.Close()
	out := map[string]string{}
	for ; it.Valid(); it.Next() {
		out[string(it.Key())] = string(it.Value())
	}
	return out
}

func runImport(t *testing.T, cfg Config) (*Report, error) {
	t.Helper()
	if cfg.MantlemintDB == "" {
		cfg.MantlemintDB = "mantlemint"
	}
	if cfg.Workers == 0 {
		cfg.Workers = 2
	}
	cfg.Logf = t.Logf
	return Run(cfg)
}

func TestRunImportServesThroughMantlemintStack(t *testing.T) {
	source := newSourceHome(t, 3)
	target := t.TempDir()

	report, err := runImport(t, Config{AppHome: source, MantlemintHome: target})
	assert.Nil(t, err)
	assert.Equal(t, int64(3), report.Height)
	assert.Equal(t, "rehearsal-1", report.ChainID)
	leaves := map[string]int64{}
	for _, s := range report.Stores {
		leaves[s.Name] = s.Leaves
	}
	assert.Equal(t, map[string]int64{"bank": 2, "empty": 0, "wasm": 1}, leaves)

	db := openImported(t, target)
	state, err := db.driver.ImportState()
	assert.Nil(t, err)
	assert.Equal(t, heleveldb.ImportStateComplete, state)
	assert.Equal(t, int64(3), db.driver.ImportFloor())

	// mantlemint resumes at the import height without running genesis
	mm := mantlemint.NewMantlemint(db.batched, nil, nil, nil, nil)
	assert.Equal(t, int64(3), mm.GetCurrentHeight())
	assert.Nil(t, mm.Init(&tendermint.GenesisDoc{ChainID: "rehearsal-1"}))

	// the app's multistore loads at the import height and serves latest reads
	rs, keys := db.loadStore(t)
	assert.Equal(t, int64(3), rs.LastCommitID().Version)
	assert.Equal(t, []byte("b3"), rs.GetKVStore(keys["bank"]).Get([]byte("b")))
	assert.Equal(t, map[string]string{"b": "b3", "c": "c2"}, storeContents(t, rs.GetKVStore(keys["bank"])))
	assert.Equal(t, map[string]string{"code": "wasm-bytes"}, storeContents(t, rs.GetKVStore(keys["wasm"])))
	assert.Empty(t, storeContents(t, rs.GetKVStore(keys["empty"])))

	// wasm code is copied byte for byte
	for _, rel := range []string{"wasm/state/wasm/checksum-1", "wasm/filesystem.lock"} {
		want, err := os.ReadFile(filepath.Join(source, "data", "wasm", rel))
		assert.Nil(t, err)
		got, err := os.ReadFile(filepath.Join(target, "data", "wasm", rel))
		assert.Nil(t, err)
		assert.Equal(t, want, got, rel)
	}
}

func TestRunImportServesHistoricalQueriesAndFloor(t *testing.T) {
	source := newSourceHome(t, 3)
	target := t.TempDir()
	_, err := runImport(t, Config{AppHome: source, MantlemintHome: target})
	assert.Nil(t, err)

	db := openImported(t, target)
	// simulate one injected block that commits version 4 without touching bank,
	// so a query at height 3 takes the height-limited path
	batched := safe_batch.NewSafeBatchDB(db.hldb).(safe_batch.SafeBatchDBCloser)
	db.hldb.SetWriteHeight(4)
	batched.Open()
	latest, err := gogotypes.StdInt64Marshal(4)
	assert.Nil(t, err)
	assert.Nil(t, batched.Set([]byte("s/latest"), latest))
	info := storetypes.CommitInfo{Version: 4}
	for _, name := range fixtureStores {
		info.StoreInfos = append(info.StoreInfos, storetypes.StoreInfo{Name: name, CommitId: storetypes.CommitID{Version: 4}})
	}
	bz, err := info.Marshal()
	assert.Nil(t, err)
	assert.Nil(t, batched.Set([]byte("s/4"), bz))
	assert.Nil(t, batched.Set([]byte("s/k:bank/d"), []byte("d4")))
	rollback, err := batched.Flush()
	assert.Nil(t, err)
	if rollback != nil {
		_ = rollback.Close()
	}
	db.hldb.ClearWriteHeight()

	rs, keys := db.loadStore(t)
	assert.Equal(t, int64(4), rs.LastCommitID().Version)

	// ?height=3: imported state, without the key added at 4
	at3, err := rs.CacheMultiStoreWithVersion(3)
	assert.Nil(t, err)
	assert.Equal(t, []byte("b3"), at3.GetKVStore(keys["bank"]).Get([]byte("b")))
	assert.Nil(t, at3.GetKVStore(keys["bank"]).Get([]byte("d")))
	assert.Equal(t, map[string]string{"b": "b3", "c": "c2"}, storeContents(t, at3.GetKVStore(keys["bank"])))

	// ?height=4 sees the new key
	assert.Equal(t, map[string]string{"b": "b3", "c": "c2", "d": "d4"}, storeContents(t, rs.GetKVStore(keys["bank"])))

	// ?height=2 is below the floor: the store adapter panics with the floor error,
	// which baseapp's query handlers recover into an error response
	at2, err := rs.CacheMultiStoreWithVersion(2)
	assert.Nil(t, err)
	assertPanicsWithFloor(t, func() { at2.GetKVStore(keys["bank"]).Get([]byte("b")) })
	assertPanicsWithFloor(t, func() { at2.GetKVStore(keys["bank"]).Iterator(nil, nil) })
}

func assertPanicsWithFloor(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		err, ok := r.(error)
		assert.True(t, ok, "expected a panic with an error, got %v", r)
		assert.True(t, errors.Is(err, heleveldb.ErrBelowImportFloor), "got %v", r)
	}()
	fn()
}

func TestRunImportsHistoricalHeight(t *testing.T) {
	source := newSourceHome(t, 2)
	target := t.TempDir()

	report, err := runImport(t, Config{AppHome: source, MantlemintHome: target, Height: 2})
	assert.Nil(t, err)
	assert.Equal(t, int64(2), report.Height)

	db := openImported(t, target)
	rs, keys := db.loadStore(t)
	assert.Equal(t, map[string]string{"a": "a1", "b": "b2", "c": "c2"}, storeContents(t, rs.GetKVStore(keys["bank"])))
}

func TestRunUsesSeparateCometHome(t *testing.T) {
	appSource := newSourceHome(t, 3)
	cometSource := t.TempDir()
	cometData := filepath.Join(cometSource, "data")
	assert.Nil(t, os.MkdirAll(cometData, 0o755))
	buildCometDB(t, cometData, "testnet-transformed", 3)

	report, err := runImport(t, Config{AppHome: appSource, CometHome: cometSource, MantlemintHome: t.TempDir()})
	assert.Nil(t, err)
	assert.Equal(t, "testnet-transformed", report.ChainID)
}

func TestRunRejectsHeightMismatchBeforeWriting(t *testing.T) {
	source := newSourceHome(t, 2) // CometBFT at 2, app state latest at 3
	target := t.TempDir()

	_, err := runImport(t, Config{AppHome: source, MantlemintHome: target})
	assert.ErrorContains(t, err, "CometBFT state is at height 2 but app state is at height 3")
	assert.False(t, errors.Is(err, ErrIncompleteImport))

	_, statErr := os.Stat(filepath.Join(target, "mantlemint.db"))
	assert.True(t, errors.Is(statErr, os.ErrNotExist))
}

func TestRunRefusesNonEmptyTarget(t *testing.T) {
	source := newSourceHome(t, 3)
	target := t.TempDir()
	_, err := runImport(t, Config{AppHome: source, MantlemintHome: target})
	assert.Nil(t, err)

	_, err = runImport(t, Config{AppHome: source, MantlemintHome: target, SkipWasm: true})
	assert.ErrorContains(t, err, "is not empty")
	assert.False(t, errors.Is(err, ErrIncompleteImport))

	db := openImported(t, target)
	state, err := db.driver.ImportState()
	assert.Nil(t, err)
	assert.Equal(t, heleveldb.ImportStateComplete, state)
}

func TestRunSkipWasm(t *testing.T) {
	source := newSourceHome(t, 3)
	target := t.TempDir()

	_, err := runImport(t, Config{AppHome: source, MantlemintHome: target, SkipWasm: true})
	assert.Nil(t, err)
	_, statErr := os.Stat(filepath.Join(target, "data", "wasm"))
	assert.True(t, errors.Is(statErr, os.ErrNotExist))
}

func TestVerifyStoreDetectsUnreadableOrDifferentEntries(t *testing.T) {
	target, err := OpenTarget(t.TempDir(), "mantlemint")
	assert.Nil(t, err)
	defer target.Close()

	w, err := target.driver.NewBulkWriter(10, 0)
	assert.Nil(t, err)
	written := newLeafDigest()
	for i := 0; i < 2; i++ {
		key := []byte(fmt.Sprintf("s/k:bank/%d", i))
		written.add(key, []byte("v"))
		assert.Nil(t, w.Set(key, []byte("v")))
	}
	// a neighbouring store's keys must not be counted
	assert.Nil(t, w.Set([]byte("s/k:bank0/x"), []byte("v")))
	assert.Nil(t, w.Close())

	assert.Nil(t, verifyStore(target.driver, "bank", 10, written))

	missing := newLeafDigest()
	for i := 0; i < 3; i++ {
		missing.add([]byte(fmt.Sprintf("s/k:bank/%d", i)), []byte("v"))
	}
	assert.ErrorContains(t, verifyStore(target.driver, "bank", 10, missing), "wrote 3 leaves but 2 are readable")

	different := newLeafDigest()
	different.add([]byte("s/k:bank/0"), []byte("v"))
	different.add([]byte("s/k:bank/1"), []byte("changed"))
	assert.ErrorContains(t, verifyStore(target.driver, "bank", 10, different), "differ from the leaves written")
}

func TestRunWithConcurrentWorkersAndSmallFlushes(t *testing.T) {
	home := t.TempDir()
	data := filepath.Join(home, "data")
	assert.Nil(t, os.MkdirAll(data, 0o755))

	stores := []string{"s0", "s1", "s2", "s3", "s4", "s5"}
	var ops []fixtureOp
	for _, s := range stores {
		for i := 0; i < 300; i++ {
			ops = append(ops, set(s, fmt.Sprintf("key-%04d", i), fmt.Sprintf("%s-value-%d", s, i)))
		}
	}
	buildAppDB(t, data, stores, [][]fixtureOp{ops})
	buildCometDB(t, data, "rehearsal-1", 1)

	report, err := runImport(t, Config{AppHome: home, MantlemintHome: t.TempDir(), Workers: 4, FlushBytes: 256, SkipWasm: true})
	assert.Nil(t, err)
	assert.Len(t, report.Stores, len(stores))
	for _, s := range report.Stores {
		assert.Equal(t, int64(300), s.Leaves, s.Name)
	}
}
