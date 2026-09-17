package rpc

import (
	"sync/atomic"

	dbm "github.com/cometbft/cometbft-db"
	sm "github.com/cometbft/cometbft/state"
	tendermint "github.com/cometbft/cometbft/types"
	"github.com/terra-money/mantlemint/db/hld"
	"github.com/terra-money/mantlemint/db/wrapped"
	"github.com/terra-money/mantlemint/indexer/block"
)

// ChainData supplies the chain data that a CometBFT node serves over RPC and
// that mantlemint keeps itself: indexed blocks and the validator sets its
// state store records.
type ChainData interface {
	// LatestHeight is the last height whose block, index and state are all written.
	LatestHeight() int64
	// Block returns the block at height, or a nil block if it is not available.
	Block(height int64) (*tendermint.Block, *tendermint.BlockID, error)
	Validators(height int64) (*tendermint.ValidatorSet, error)
	IsSynced() bool
}

// SyncedChainData reads blocks from the block indexer and validator sets from
// the CometBFT state store. The sync loop reports each height once it is
// flushed, so readers never see a height that is only partially written.
type SyncedChainData struct {
	indexerDB dbm.DB
	hldb      *hld.HeightLimitedDB
	isSynced  func() bool
	latest    atomic.Int64
}

func NewSyncedChainData(indexerDB dbm.DB, hldb *hld.HeightLimitedDB, isSynced func() bool, latestHeight int64) *SyncedChainData {
	c := &SyncedChainData{indexerDB: indexerDB, hldb: hldb, isSynced: isSynced}
	c.latest.Store(latestHeight)
	return c
}

// SetLatestHeight records that height is fully written.
func (c *SyncedChainData) SetLatestHeight(height int64) {
	c.latest.Store(height)
}

func (c *SyncedChainData) LatestHeight() int64 {
	return c.latest.Load()
}

func (c *SyncedChainData) Block(height int64) (*tendermint.Block, *tendermint.BlockID, error) {
	return block.LoadBlock(c.indexerDB, height)
}

// Validators reads through a view limited to the latest flushed height, so a
// block being applied concurrently is never visible.
func (c *SyncedChainData) Validators(height int64) (*tendermint.ValidatorSet, error) {
	view := c.hldb.BranchHeightLimitedDB(c.LatestHeight())
	return sm.NewStore(wrapped.NewWrappedDB(view), sm.StoreOptions{}).LoadValidators(height)
}

func (c *SyncedChainData) IsSynced() bool {
	return c.isSynced()
}
