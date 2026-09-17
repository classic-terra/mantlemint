package importer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	dbm "github.com/cometbft/cometbft-db"
	sm "github.com/cometbft/cometbft/state"
	cosmosdb "github.com/cosmos/cosmos-db"
	"github.com/cosmos/iavl"
	idb "github.com/cosmos/iavl/db"
	"github.com/stretchr/testify/assert"
	"github.com/terra-money/mantlemint/db/heleveldb"
)

func assertNoTargetCreated(t *testing.T, target string) {
	t.Helper()
	_, err := os.Stat(filepath.Join(target, "mantlemint.db"))
	assert.True(t, errors.Is(err, os.ErrNotExist), "target database must not be created, stat: %v", err)
}

func TestRunPreflightRejectsExistingWasmDestination(t *testing.T) {
	source := newSourceHome(t, 3)
	target := t.TempDir()
	assert.Nil(t, os.MkdirAll(filepath.Join(target, "data", "wasm"), 0o755))

	_, err := runImport(t, Config{AppHome: source, MantlemintHome: target})
	assert.ErrorContains(t, err, "already exists")
	assert.False(t, errors.Is(err, ErrIncompleteImport))
	assertNoTargetCreated(t, target)
}

func TestRunPreflightRejectsMissingWasmSource(t *testing.T) {
	source := newSourceHome(t, 3)
	assert.Nil(t, os.RemoveAll(filepath.Join(source, "data", "wasm")))
	target := t.TempDir()

	_, err := runImport(t, Config{AppHome: source, MantlemintHome: target})
	assert.ErrorContains(t, err, "skip-wasm")
	assert.False(t, errors.Is(err, ErrIncompleteImport))
	assertNoTargetCreated(t, target)
}

func TestRunPreflightRejectsVoteExtensions(t *testing.T) {
	source := newSourceHome(t, 3)
	data := filepath.Join(source, "data")

	stateDB, err := dbm.NewGoLevelDB("state", data)
	assert.Nil(t, err)
	st, err := sm.NewStore(stateDB, sm.StoreOptions{}).Load()
	assert.Nil(t, err)
	st.ConsensusParams.ABCI.VoteExtensionsEnableHeight = 1
	assert.Nil(t, sm.NewStore(stateDB, sm.StoreOptions{}).Save(st))
	assert.Nil(t, stateDB.Close())

	target := t.TempDir()
	_, err = runImport(t, Config{AppHome: source, MantlemintHome: target})
	assert.ErrorContains(t, err, "vote extensions are enabled")
	assert.False(t, errors.Is(err, ErrIncompleteImport))
	assertNoTargetCreated(t, target)
}

// A store that cannot be read at the import height fails while other stores are
// importing concurrently; the target stays marked in progress with no floor.
func TestRunConcurrentStoreFailureLeavesImportInProgress(t *testing.T) {
	source := newSourceHome(t, 2)
	data := filepath.Join(source, "data")

	appDB, err := cosmosdb.NewGoLevelDB(appDBName, data, nil)
	assert.Nil(t, err)
	tree := iavl.NewMutableTree(idb.NewWrapper(cosmosdb.NewPrefixDB(appDB, []byte("s/k:bank/"))), 0, false, iavl.NewNopLogger())
	_, err = tree.LoadVersion(3)
	assert.Nil(t, err)
	assert.Nil(t, tree.DeleteVersionsTo(2))
	assert.Nil(t, appDB.Close())

	target := t.TempDir()
	_, err = runImport(t, Config{AppHome: source, MantlemintHome: target, Height: 2, Workers: 3, SkipWasm: true})
	assert.True(t, errors.Is(err, ErrIncompleteImport), "got %v", err)
	assert.ErrorContains(t, err, `store "bank": version 2 is not available`)

	db := openImported(t, target)
	state, err := db.driver.ImportState()
	assert.Nil(t, err)
	assert.Equal(t, heleveldb.ImportStateInProgress, state)
	assert.Equal(t, int64(0), db.driver.ImportFloor())
}

func TestCopyWasmDirResolvesSymlinkedRoot(t *testing.T) {
	real := filepath.Join(t.TempDir(), "wasm-on-other-disk")
	assert.Nil(t, os.MkdirAll(filepath.Join(real, "state"), 0o755))
	assert.Nil(t, os.WriteFile(filepath.Join(real, "state", "code"), []byte("contract"), 0o644))

	src := filepath.Join(t.TempDir(), "wasm")
	assert.Nil(t, os.Symlink(real, src))
	dst := filepath.Join(t.TempDir(), "data", "wasm")

	assert.Nil(t, copyWasmDir(context.Background(), src, dst))

	info, err := os.Lstat(dst)
	assert.Nil(t, err)
	assert.True(t, info.IsDir(), "destination must be a real directory, not a link into the source")
	got, err := os.ReadFile(filepath.Join(dst, "state", "code"))
	assert.Nil(t, err)
	assert.Equal(t, []byte("contract"), got)

	// writing into the copy must not touch the source
	assert.Nil(t, os.WriteFile(filepath.Join(dst, "state", "cache"), []byte("x"), 0o644))
	_, err = os.Stat(filepath.Join(real, "state", "cache"))
	assert.True(t, errors.Is(err, os.ErrNotExist))
}

func TestCopyWasmDirRejectsSymlinksInside(t *testing.T) {
	src := filepath.Join(t.TempDir(), "wasm")
	assert.Nil(t, os.MkdirAll(src, 0o755))
	assert.Nil(t, os.Symlink(t.TempDir(), filepath.Join(src, "linked")))

	err := copyWasmDir(context.Background(), src, filepath.Join(t.TempDir(), "wasm"))
	assert.ErrorContains(t, err, "symlink")
}
