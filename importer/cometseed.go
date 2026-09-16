package importer

import (
	"fmt"

	dbm "github.com/cometbft/cometbft-db"
	sm "github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/store"
	"github.com/cometbft/cometbft/types"
	"github.com/syndtr/goleveldb/leveldb/filter"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

// CometSource reads CometBFT state from a stopped node's state.db and blockstore.db.
type CometSource struct {
	stateDB dbm.DB
	blockDB dbm.DB
	state   sm.State
	blocks  *store.BlockStore
}

func OpenCometState(dataDir string) (*CometSource, error) {
	stateDB, err := openReadOnly("state", dataDir)
	if err != nil {
		return nil, err
	}
	blockDB, err := openReadOnly("blockstore", dataDir)
	if err != nil {
		_ = stateDB.Close()
		return nil, err
	}

	src := &CometSource{stateDB: stateDB, blockDB: blockDB}
	if err := src.load(); err != nil {
		_ = src.Close()
		return nil, err
	}
	return src, nil
}

func openReadOnly(name, dir string) (dbm.DB, error) {
	db, err := dbm.NewGoLevelDBWithOpts(name, dir, &opt.Options{
		ReadOnly: true,
		Filter:   filter.NewBloomFilter(10),
	})
	if err != nil {
		return nil, fmt.Errorf("open %s.db in %s read-only (is the node still running?): %w", name, dir, err)
	}
	return db, nil
}

func (c *CometSource) load() (err error) {
	defer recoverInto(&err, "load CometBFT state")

	c.state, err = sm.NewStore(c.stateDB, sm.StoreOptions{}).Load()
	if err != nil {
		return fmt.Errorf("load CometBFT state: %w", err)
	}
	if c.state.IsEmpty() {
		return fmt.Errorf("no CometBFT state in state.db; point the CometBFT home at the node that produces the blocks")
	}
	c.blocks = store.NewBlockStore(c.blockDB)
	return nil
}

// Height is the last block height of the CometBFT state.
func (c *CometSource) Height() int64 {
	return c.state.LastBlockHeight
}

func (c *CometSource) ChainID() string {
	return c.state.ChainID
}

// SeedInto writes the CometBFT state and block at height into target, so that
// mantlemint resumes at height and validates block height+1.
func (c *CometSource) SeedInto(target dbm.DB, height int64) (err error) {
	defer recoverInto(&err, "seed CometBFT state")

	if c.state.LastBlockHeight != height {
		return fmt.Errorf("CometBFT state is at height %d but app state is at height %d; both sources must be at the same height",
			c.state.LastBlockHeight, height)
	}
	if c.state.ConsensusParams.ABCI.VoteExtensionsEnabled(height) {
		return fmt.Errorf("vote extensions are enabled at height %d; seeding the required extended commit is not supported", height)
	}

	block := c.blocks.LoadBlock(height)
	meta := c.blocks.LoadBlockMeta(height)
	seen := c.blocks.LoadSeenCommit(height)
	if block == nil || meta == nil || seen == nil {
		return fmt.Errorf("block %d or its seen commit is missing from blockstore.db", height)
	}
	parts, err := block.MakePartSet(types.BlockPartSizeBytes)
	if err != nil {
		return fmt.Errorf("split block %d into parts: %w", height, err)
	}
	if !parts.Header().Equals(meta.BlockID.PartSetHeader) {
		return fmt.Errorf("block %d part set does not match its stored block ID", height)
	}

	st := c.state.Copy()
	// Bootstrap stores full validator sets at height, height+1 and height+2, and
	// consensus params at height+1. Point the "last changed" heights at those
	// records, as CometBFT statesync does. Left at the source's older heights,
	// later saves write pointer records to heights that were never imported, and
	// loading validators panics a few blocks after the import.
	st.LastHeightValidatorsChanged = height + 2
	st.LastHeightConsensusParamsChanged = height + 1

	if err := sm.NewStore(target, sm.StoreOptions{}).Bootstrap(st); err != nil {
		return fmt.Errorf("bootstrap CometBFT state: %w", err)
	}
	// SaveBlock panics on failure; recovered above
	store.NewBlockStore(target).SaveBlock(block, parts, seen)
	return nil
}

func (c *CometSource) Close() error {
	err := c.stateDB.Close()
	if blockErr := c.blockDB.Close(); err == nil {
		err = blockErr
	}
	return err
}

func recoverInto(err *error, action string) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("%s: %v", action, r)
	}
}
