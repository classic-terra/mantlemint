package rootmulti

import (
	"io"

	"cosmossdk.io/store/cachekv"
	"cosmossdk.io/store/tracekv"
	"cosmossdk.io/store/types"

	pruningtypes "cosmossdk.io/store/pruning/types"
	cosmosdb "github.com/cosmos/cosmos-db"
	dbm "github.com/cometbft/cometbft-db"
)

var commithash = []byte("FAKE_HASH")

//----------------------------------------
// commitDBStoreWrapper should only be used for simulation/debugging,
// as it doesn't compute any commit hash, and it cannot load older state.

// Wrapper type for dbm.Db with implementation of KVStore
type commitDBStoreAdapter struct {
	db     cosmosdb.DB
	prefix []byte
}

func (cdsa commitDBStoreAdapter) Commit() types.CommitID {
	return types.CommitID{
		Version: -1,
		Hash:    commithash,
	}
}

func (cdsa commitDBStoreAdapter) LastCommitID() types.CommitID {
	return types.CommitID{
		Version: -1,
		Hash:    commithash,
	}
}
func (cdsa commitDBStoreAdapter) SetPruning(_ pruningtypes.PruningOptions) {}

// GetPruning is a no-op as pruning options cannot be directly set on this store.
// They must be set on the root commit multi-store.
func (cdsa commitDBStoreAdapter) GetPruning() pruningtypes.PruningOptions {
	return pruningtypes.NewPruningOptions(pruningtypes.PruningUndefined)
}

func (cdsa commitDBStoreAdapter) WorkingHash() []byte {
	return commithash
}

func (cdsa commitDBStoreAdapter) Get(key []byte) []byte {
	value, err := cdsa.db.Get(key)
	if err != nil {
		panic(err)
	}

	return value
}

func (cdsa commitDBStoreAdapter) Has(key []byte) bool {
	ok, err := cdsa.db.Has(key)
	if err != nil {
		panic(err)
	}

	return ok
}

func (cdsa commitDBStoreAdapter) Set(key, value []byte) {
	types.AssertValidKey(key)
	types.AssertValidValue(value)
	if err := cdsa.db.Set(key, value); err != nil {
		panic(err)
	}
}

func (cdsa commitDBStoreAdapter) Delete(key []byte) {
	if err := cdsa.db.Delete(key); err != nil {
		panic(err)
	}
}

func (cdsa commitDBStoreAdapter) Iterator(start, end []byte) types.Iterator {
	iter, err := cdsa.db.Iterator(start, end)
	if err != nil {
		panic(err)
	}

	return iter
}

func (cdsa commitDBStoreAdapter) ReverseIterator(start, end []byte) types.Iterator {
	iter, err := cdsa.db.ReverseIterator(start, end)
	if err != nil {
		panic(err)
	}

	return iter
}

func (commitDBStoreAdapter) GetStoreType() types.StoreType {
	return types.StoreTypeDB
}

func (cdsa commitDBStoreAdapter) CacheWrap() types.CacheWrap {
	return cachekv.NewStore(cdsa)
}

func (cdsa commitDBStoreAdapter) CacheWrapWithTrace(w io.Writer, tc types.TraceContext) types.CacheWrap {
	return cachekv.NewStore(tracekv.NewStore(cdsa, w, tc))
}

func (cdsa *commitDBStoreAdapter) BranchStoreWithHeightLimitedDB(hldb dbm.DB) types.CommitKVStore {
	db := cosmosdb.NewPrefixDB(newCosmosDBAdapter(hldb), cdsa.prefix)

	return commitDBStoreAdapter{db: db, prefix: cdsa.prefix}
}
