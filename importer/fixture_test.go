package importer

import (
	"fmt"
	"testing"

	storetypes "cosmossdk.io/store/types"
	dbm "github.com/cosmos/cosmos-db"
	gogotypes "github.com/cosmos/gogoproto/types"
	"github.com/cosmos/iavl"
	idb "github.com/cosmos/iavl/db"
	"github.com/stretchr/testify/assert"
)

// fixtureOp is a single change applied to a store in one version; a nil value removes the key.
type fixtureOp struct {
	store string
	key   string
	value *string
}

func set(store, key, value string) fixtureOp { return fixtureOp{store: store, key: key, value: &value} }
func del(store, key string) fixtureOp        { return fixtureOp{store: store, key: key} }

// buildAppDB writes a terrad-shaped application.db into dataDir: one iavl tree per
// store under s/k:<name>/, commit info at s/<version>, and s/latest.
// versions[i] holds the ops committed as version i+1.
func buildAppDB(t *testing.T, dataDir string, stores []string, versions [][]fixtureOp) {
	t.Helper()
	db, err := dbm.NewGoLevelDB(appDBName, dataDir, nil)
	assert.Nil(t, err)
	defer db.Close()

	trees := map[string]*iavl.MutableTree{}
	for _, name := range stores {
		prefixed := dbm.NewPrefixDB(db, []byte(fmt.Sprintf(storeKeyPrefix, name)))
		// like terrad: fast storage enabled
		trees[name] = iavl.NewMutableTree(idb.NewWrapper(prefixed), 0, false, iavl.NewNopLogger())
	}

	for i, ops := range versions {
		version := int64(i + 1)
		for _, op := range ops {
			tree := trees[op.store]
			if op.value == nil {
				_, _, err := tree.Remove([]byte(op.key))
				assert.Nil(t, err)
			} else {
				_, err := tree.Set([]byte(op.key), []byte(*op.value))
				assert.Nil(t, err)
			}
		}

		info := storetypes.CommitInfo{Version: version}
		for _, name := range stores {
			hash, v, err := trees[name].SaveVersion()
			assert.Nil(t, err)
			assert.Equal(t, version, v)
			info.StoreInfos = append(info.StoreInfos, storetypes.StoreInfo{
				Name:     name,
				CommitId: storetypes.CommitID{Version: v, Hash: hash},
			})
		}
		bz, err := info.Marshal()
		assert.Nil(t, err)
		assert.Nil(t, db.Set([]byte(fmt.Sprintf(commitInfoKeyFmt, version)), bz))
		latest, err := gogotypes.StdInt64Marshal(version)
		assert.Nil(t, err)
		assert.Nil(t, db.Set([]byte(latestVersionKey), latest))
	}
}

// standardFixture has three versions across a changing store, a static store and an empty store.
func standardFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	buildAppDB(t, dir, []string{"bank", "empty", "wasm"}, [][]fixtureOp{
		{set("bank", "a", "a1"), set("bank", "b", "b1"), set("wasm", "code", "wasm-bytes")},
		{set("bank", "b", "b2"), set("bank", "c", "c2")},
		{set("bank", "b", "b3"), del("bank", "a")},
	})
	return dir
}

func collectStore(t *testing.T, src *AppStateSource, name string) (map[string]string, int64) {
	t.Helper()
	got := map[string]string{}
	count, err := src.IterateStore(name, func(key, value []byte) error {
		got[string(key)] = string(value)
		return nil
	})
	assert.Nil(t, err)
	return got, count
}
