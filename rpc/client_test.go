package rpc

import (
	"context"
	"errors"
	"testing"
	"time"

	sm "github.com/cometbft/cometbft/state"
	tendermint "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/assert"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/terra-money/mantlemint/db/heleveldb"
	"github.com/terra-money/mantlemint/db/hld"
	"github.com/terra-money/mantlemint/db/wrapped"
)

type fakeChain struct {
	latest     int64
	synced     bool
	blocks     map[int64]*tendermint.Block
	validators map[int64]*tendermint.ValidatorSet
}

func (f *fakeChain) LatestHeight() int64 { return f.latest }
func (f *fakeChain) IsSynced() bool      { return f.synced }

func (f *fakeChain) Block(height int64) (*tendermint.Block, *tendermint.BlockID, error) {
	b, ok := f.blocks[height]
	if !ok {
		return nil, nil, nil
	}
	return b, &tendermint.BlockID{Hash: b.Hash()}, nil
}

func (f *fakeChain) Validators(height int64) (*tendermint.ValidatorSet, error) {
	v, ok := f.validators[height]
	if !ok {
		return nil, errors.New("no validators")
	}
	return v, nil
}

func newFakeChain(latest int64) *fakeChain {
	f := &fakeChain{latest: latest, blocks: map[int64]*tendermint.Block{}, validators: map[int64]*tendermint.ValidatorSet{}}
	for h := latest - 1; h <= latest; h++ {
		f.blocks[h] = &tendermint.Block{Header: tendermint.Header{
			ChainID: "columbus-5", Height: h, Time: time.Unix(h, 0).UTC(), AppHash: []byte{byte(h)},
		}}
	}
	return f
}

func height(h int64) *int64 { return &h }
func intp(i int) *int       { return &i }

func TestClientBlock(t *testing.T) {
	chain := newFakeChain(100)
	c := NewRpcClient(nil, chain)

	latest, err := c.Block(context.Background(), nil)
	assert.Nil(t, err)
	assert.Equal(t, int64(100), latest.Block.Height)
	assert.Equal(t, latest.Block.Hash(), latest.BlockID.Hash)

	earlier, err := c.Block(context.Background(), height(99))
	assert.Nil(t, err)
	assert.Equal(t, int64(99), earlier.Block.Height)

	_, err = c.Block(context.Background(), height(101))
	assert.ErrorContains(t, err, "must be less than or equal to the current blockchain height 100")
	_, err = c.Block(context.Background(), height(0))
	assert.ErrorContains(t, err, "must be greater than 0")
	// below an import height, or never indexed
	_, err = c.Block(context.Background(), height(50))
	assert.ErrorContains(t, err, "block at height 50 is not available")
}

func TestClientStatus(t *testing.T) {
	chain := newFakeChain(100)
	c := NewRpcClient(nil, chain)

	status, err := c.Status(context.Background())
	assert.Nil(t, err)
	assert.Equal(t, int64(100), status.SyncInfo.LatestBlockHeight)
	assert.Equal(t, chain.blocks[100].Hash(), status.SyncInfo.LatestBlockHash)
	assert.Equal(t, []byte{100}, []byte(status.SyncInfo.LatestAppHash))
	assert.Equal(t, time.Unix(100, 0).UTC(), status.SyncInfo.LatestBlockTime)
	assert.True(t, status.SyncInfo.CatchingUp)

	chain.synced = true
	status, err = c.Status(context.Background())
	assert.Nil(t, err)
	assert.False(t, status.SyncInfo.CatchingUp)
}

func TestClientValidators(t *testing.T) {
	chain := newFakeChain(100)
	set, _ := tendermint.RandValidatorSet(35, 10)
	chain.validators[100] = set
	chain.validators[101] = set
	c := NewRpcClient(nil, chain)
	ctx := context.Background()

	// catching up: the latest set is the one for the latest block
	res, err := c.Validators(ctx, nil, nil, nil)
	assert.Nil(t, err)
	assert.Equal(t, int64(100), res.BlockHeight)
	assert.Equal(t, 30, res.Count)
	assert.Equal(t, 35, res.Total)
	assert.Equal(t, set.Validators[:30], res.Validators)

	// synced: the next height's set, as CometBFT serves it
	chain.synced = true
	res, err = c.Validators(ctx, nil, nil, nil)
	assert.Nil(t, err)
	assert.Equal(t, int64(101), res.BlockHeight)

	res, err = c.Validators(ctx, height(100), intp(2), intp(30))
	assert.Nil(t, err)
	assert.Equal(t, set.Validators[30:], res.Validators)
	assert.Equal(t, 5, res.Count)

	res, err = c.Validators(ctx, height(100), nil, intp(500))
	assert.Nil(t, err)
	assert.Equal(t, 35, res.Count, "per page is capped at 100")

	_, err = c.Validators(ctx, height(100), intp(3), intp(30))
	assert.ErrorContains(t, err, "page should be within [1, 2] range, given 3")
	_, err = c.Validators(ctx, height(102), nil, nil)
	assert.ErrorContains(t, err, "current blockchain height 101")
}

// TestSyncedChainDataValidators reads validator sets the state store wrote
// through mantlemint's height-limited database, and hides a height until the
// sync loop reports it as flushed.
func TestSyncedChainDataValidators(t *testing.T) {
	driver, err := heleveldb.NewLevelDBDriver(&heleveldb.DriverConfig{
		Name: "mantlemint", Dir: t.TempDir(), Mode: heleveldb.DriverModeKeySuffixDesc, Options: &opt.Options{},
	})
	assert.Nil(t, err)
	defer driver.Close()
	hldb := hld.ApplyHeightLimitedDB(driver, &hld.HeightLimitedDBConfig{})

	set, _ := tendermint.RandValidatorSet(4, 10)
	state := sm.State{
		ChainID: "columbus-5", InitialHeight: 1, LastBlockHeight: 10,
		Validators: set, NextValidators: set, LastValidators: set, LastHeightValidatorsChanged: 1,
	}
	hldb.SetWriteHeight(10)
	assert.Nil(t, sm.NewStore(wrapped.NewWrappedDB(hldb), sm.StoreOptions{}).Bootstrap(state))
	hldb.ClearWriteHeight()

	chain := NewSyncedChainData(nil, hldb, func() bool { return true }, 9)
	_, err = chain.Validators(11)
	assert.NotNil(t, err, "height 10 is not reported as flushed yet")

	chain.SetLatestHeight(10)
	got, err := chain.Validators(11)
	assert.Nil(t, err)
	assert.Equal(t, set.Hash(), got.Hash())
}
