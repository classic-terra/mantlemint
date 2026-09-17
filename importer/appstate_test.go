package importer

import (
	"errors"
	"testing"

	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/iavl"
	idb "github.com/cosmos/iavl/db"
	"github.com/stretchr/testify/assert"
)

func TestAppStateLatestVersion(t *testing.T) {
	src, err := OpenAppState(standardFixture(t), 0)
	assert.Nil(t, err)
	defer src.Close()

	assert.Equal(t, int64(3), src.Height())
	assert.Equal(t, []string{"bank", "empty", "wasm"}, src.StoreNames())

	bank, count := collectStore(t, src, "bank")
	assert.Equal(t, map[string]string{"b": "b3", "c": "c2"}, bank)
	assert.Equal(t, int64(2), count)

	wasm, count := collectStore(t, src, "wasm")
	assert.Equal(t, map[string]string{"code": "wasm-bytes"}, wasm)
	assert.Equal(t, int64(1), count)
}

func TestAppStateHistoricalVersion(t *testing.T) {
	src, err := OpenAppState(standardFixture(t), 2)
	assert.Nil(t, err)
	defer src.Close()

	assert.Equal(t, int64(2), src.Height())
	bank, count := collectStore(t, src, "bank")
	// version 2 still has a, and b's value from before version 3 changed it
	assert.Equal(t, map[string]string{"a": "a1", "b": "b2", "c": "c2"}, bank)
	assert.Equal(t, int64(3), count)
}

func TestAppStateKeysInOrder(t *testing.T) {
	src, err := OpenAppState(standardFixture(t), 2)
	assert.Nil(t, err)
	defer src.Close()

	var keys []string
	_, err = src.IterateStore("bank", func(key, _ []byte) error {
		keys = append(keys, string(key))
		return nil
	})
	assert.Nil(t, err)
	assert.Equal(t, []string{"a", "b", "c"}, keys)
}

func TestAppStateEmptyStore(t *testing.T) {
	src, err := OpenAppState(standardFixture(t), 0)
	assert.Nil(t, err)
	defer src.Close()

	got, count := collectStore(t, src, "empty")
	assert.Empty(t, got)
	assert.Equal(t, int64(0), count)
}

func TestAppStateCallbackErrorStopsIteration(t *testing.T) {
	src, err := OpenAppState(standardFixture(t), 0)
	assert.Nil(t, err)
	defer src.Close()

	boom := errors.New("write failed")
	_, err = src.IterateStore("bank", func(_, _ []byte) error { return boom })
	assert.True(t, errors.Is(err, boom))
}

func TestAppStateHeightWithoutCommitInfo(t *testing.T) {
	dir := standardFixture(t)
	_, err := OpenAppState(dir, 4)
	assert.ErrorContains(t, err, "above the latest committed version 3")

	// commit info removed for version 1, as pruning of metadata would leave it
	db, err := dbm.NewGoLevelDB(appDBName, dir, nil)
	assert.Nil(t, err)
	assert.Nil(t, db.Delete([]byte("s/1")))
	assert.Nil(t, db.Close())

	_, err = OpenAppState(dir, 1)
	assert.ErrorContains(t, err, "no commit info at height 1")
}

func TestAppStatePrunedVersion(t *testing.T) {
	dir := standardFixture(t)

	// prune version 1 out of the bank tree while its commit info remains
	db, err := dbm.NewGoLevelDB(appDBName, dir, nil)
	assert.Nil(t, err)
	tree := iavl.NewMutableTree(idb.NewWrapper(dbm.NewPrefixDB(db, []byte("s/k:bank/"))), 0, false, iavl.NewNopLogger())
	_, err = tree.LoadVersion(3)
	assert.Nil(t, err)
	assert.Nil(t, tree.DeleteVersionsTo(1))
	assert.Nil(t, db.Close())

	src, err := OpenAppState(dir, 1)
	assert.Nil(t, err)
	defer src.Close()

	_, err = src.IterateStore("bank", func(_, _ []byte) error { return nil })
	assert.ErrorContains(t, err, `store "bank": version 1 is not available`)
}

func TestAppStateNotAnApplicationDB(t *testing.T) {
	dir := t.TempDir()
	db, err := dbm.NewGoLevelDB(appDBName, dir, nil)
	assert.Nil(t, err)
	assert.Nil(t, db.Set([]byte("unrelated"), []byte("x")))
	assert.Nil(t, db.Close())

	_, err = OpenAppState(dir, 0)
	assert.ErrorContains(t, err, "no committed version found")
}

func TestAppStateMissingDirectory(t *testing.T) {
	_, err := OpenAppState(t.TempDir()+"/does-not-exist", 0)
	assert.NotNil(t, err)
}

func TestAppStateRefusesLockedDatabase(t *testing.T) {
	dir := standardFixture(t)

	// a running terrad holds the database open for writing
	running, err := dbm.NewGoLevelDB(appDBName, dir, nil)
	assert.Nil(t, err)

	_, err = OpenAppState(dir, 0)
	assert.ErrorContains(t, err, "is terrad still running?")

	assert.Nil(t, running.Close())

	// nothing was modified: the data is still readable afterwards
	src, err := OpenAppState(dir, 0)
	assert.Nil(t, err)
	defer src.Close()
	bank, _ := collectStore(t, src, "bank")
	assert.Equal(t, map[string]string{"b": "b3", "c": "c2"}, bank)
}
