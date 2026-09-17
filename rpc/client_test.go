package rpc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	sm "github.com/cometbft/cometbft/state"
	tendermint "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/assert"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/terra-money/mantlemint/db/heleveldb"
	"github.com/terra-money/mantlemint/db/hld"
	"github.com/terra-money/mantlemint/db/safe_batch"
	"github.com/terra-money/mantlemint/db/wrapped"
)

type fakeChain struct {
	latest     int64
	synced     bool
	blocks     map[int64]*tendermint.Block
	validators map[int64]*tendermint.ValidatorSet
	results    map[int64]*abci.ResponseFinalizeBlock
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

func (f *fakeChain) BlockResults(height int64) (*abci.ResponseFinalizeBlock, error) {
	r, ok := f.results[height]
	if !ok {
		return nil, errors.New("no results")
	}
	return r, nil
}

func (f *fakeChain) TxLocation(hash []byte) (int64, uint32, bool, error) {
	for h, b := range f.blocks {
		for i, tx := range b.Txs {
			if bytes.Equal(tx.Hash(), hash) {
				return h, uint32(i), true, nil
			}
		}
	}
	return 0, 0, false, nil
}

// newFakeChain holds blocks latest-1 and latest; each has three txs with results.
func newFakeChain(latest int64) *fakeChain {
	f := &fakeChain{
		latest: latest, blocks: map[int64]*tendermint.Block{}, validators: map[int64]*tendermint.ValidatorSet{},
		results: map[int64]*abci.ResponseFinalizeBlock{},
	}
	for h := latest - 1; h <= latest; h++ {
		block := &tendermint.Block{Header: tendermint.Header{
			ChainID: "columbus-5", Height: h, Time: time.Unix(h, 0).UTC(), AppHash: []byte{byte(h)},
		}}
		results := &abci.ResponseFinalizeBlock{AppHash: []byte{byte(h)}}
		for i := 0; i < 3; i++ {
			block.Txs = append(block.Txs, tendermint.Tx(fmt.Sprintf("tx-%d-%d", h, i)))
			results.TxResults = append(results.TxResults, &abci.ExecTxResult{GasUsed: h*10 + int64(i)})
		}
		f.blocks[h] = block
		f.results[h] = results
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

func TestClientTx(t *testing.T) {
	chain := newFakeChain(100)
	c := NewRpcClient(nil, chain)
	ctx := context.Background()
	want := chain.blocks[99].Txs[2]

	res, err := c.Tx(ctx, want.Hash(), true)
	assert.Nil(t, err)
	assert.Equal(t, int64(99), res.Height)
	assert.Equal(t, uint32(2), res.Index)
	assert.Equal(t, want, res.Tx)
	assert.Equal(t, int64(992), res.TxResult.GasUsed)
	assert.Nil(t, res.Proof.Validate(chain.blocks[99].Txs.Hash()), "proof verifies against the block's tx root")

	_, err = c.Tx(ctx, tendermint.Tx("unknown").Hash(), false)
	assert.ErrorContains(t, err, "not found")
}

func TestClientBlockResults(t *testing.T) {
	chain := newFakeChain(100)
	c := NewRpcClient(nil, chain)

	res, err := c.BlockResults(context.Background(), nil)
	assert.Nil(t, err)
	assert.Equal(t, int64(100), res.Height)
	assert.Len(t, res.TxsResults, 3)
	assert.Equal(t, []byte{100}, []byte(res.AppHash))

	_, err = c.BlockResults(context.Background(), height(101))
	assert.ErrorContains(t, err, "current blockchain height 100")
}

func TestClientTxSearch(t *testing.T) {
	chain := newFakeChain(100)
	c := NewRpcClient(nil, chain)
	ctx := context.Background()
	hashes := func(res *coretypes.ResultTxSearch) []tendermint.Tx {
		var txs []tendermint.Tx
		for _, r := range res.Txs {
			txs = append(txs, r.Tx)
		}
		return txs
	}
	block := chain.blocks[99].Txs

	res, err := c.TxSearch(ctx, "tx.height=99", false, nil, nil, "")
	assert.Nil(t, err)
	assert.Equal(t, 3, res.TotalCount)
	assert.Equal(t, []tendermint.Tx{block[0], block[1], block[2]}, hashes(res))

	res, err = c.TxSearch(ctx, "tx.height = 99", false, intp(2), intp(2), "desc")
	assert.Nil(t, err)
	assert.Equal(t, 3, res.TotalCount)
	assert.Equal(t, []tendermint.Tx{block[0]}, hashes(res), "second page of the descending order")

	res, err = c.TxSearch(ctx, fmt.Sprintf("tx.hash='%X'", block[1].Hash()), false, nil, nil, "")
	assert.Nil(t, err)
	assert.Equal(t, []tendermint.Tx{block[1]}, hashes(res))

	// heights and hashes that are not indexed match nothing, as with an event index
	for _, q := range []string{"tx.height=101", "tx.height=5", fmt.Sprintf("tx.hash='%X'", tendermint.Tx("x").Hash())} {
		res, err = c.TxSearch(ctx, q, false, nil, nil, "")
		assert.Nil(t, err, q)
		assert.Equal(t, 0, res.TotalCount, q)
	}

	for _, q := range []string{"message.sender='terra1x'", "tx.height>5", "tx.height=99 AND message.action='send'", "tx.hash=5"} {
		_, err = c.TxSearch(ctx, q, false, nil, nil, "")
		assert.ErrorIs(t, err, errUnsupportedQuery, q)
	}
	_, err = c.TxSearch(ctx, "tx.height=99", false, nil, nil, "sideways")
	assert.ErrorContains(t, err, "order_by")
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

	// block results follow the same flushed-height view
	// written the way the sync loop writes a block: through the batched layer, then flushed
	batched := safe_batch.NewSafeBatchDB(hldb).(safe_batch.SafeBatchDBCloser)
	hldb.SetWriteHeight(11)
	batched.Open()
	results := &abci.ResponseFinalizeBlock{TxResults: []*abci.ExecTxResult{{GasUsed: 7}}, AppHash: []byte("app")}
	assert.Nil(t, sm.NewStore(wrapped.NewWrappedDB(batched), sm.StoreOptions{}).SaveFinalizeBlockResponse(11, results))
	_, err = batched.Flush()
	assert.Nil(t, err)
	hldb.ClearWriteHeight()
	_, err = chain.BlockResults(11)
	assert.NotNil(t, err, "height 11 is not reported as flushed yet")
	chain.SetLatestHeight(11)
	gotResults, err := chain.BlockResults(11)
	assert.Nil(t, err)
	assert.Equal(t, int64(7), gotResults.TxResults[0].GasUsed)
}
