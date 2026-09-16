package importer

import (
	"testing"
	"time"

	dbm "github.com/cometbft/cometbft-db"
	"github.com/cometbft/cometbft/crypto/ed25519"
	sm "github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/store"
	"github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/assert"
	"github.com/terra-money/mantlemint/db/wrapped"
)

type cometFixture struct {
	state     sm.State
	blockHash []byte
}

// buildCometDB writes state.db and blockstore.db for a node that has been running
// long enough that its validators and consensus params last changed at height 1.
func buildCometDB(t *testing.T, dataDir, chainID string, height int64) cometFixture {
	t.Helper()
	stateDB, err := dbm.NewGoLevelDB("state", dataDir)
	assert.Nil(t, err)
	defer stateDB.Close()
	blockDB, err := dbm.NewGoLevelDB("blockstore", dataDir)
	assert.Nil(t, err)
	defer blockDB.Close()

	vals := types.NewValidatorSet([]*types.Validator{
		types.NewValidator(ed25519.GenPrivKey().PubKey(), 10),
		types.NewValidator(ed25519.GenPrivKey().PubKey(), 20),
	})

	lastCommit := &types.Commit{
		Height:     height - 1,
		BlockID:    types.BlockID{Hash: []byte("previous-block-hash-000000000000")},
		Signatures: []types.CommitSig{types.NewCommitSigAbsent(), types.NewCommitSigAbsent()},
	}
	block := types.MakeBlock(height, []types.Tx{types.Tx("tx")}, lastCommit, nil)
	block.ChainID = chainID
	block.Time = time.Unix(1_700_000_000, 0).UTC()
	block.ValidatorsHash = vals.Hash()
	block.NextValidatorsHash = vals.Hash()
	block.ProposerAddress = vals.Validators[0].Address
	parts, err := block.MakePartSet(types.BlockPartSizeBytes)
	assert.Nil(t, err)
	blockID := types.BlockID{Hash: block.Hash(), PartSetHeader: parts.Header()}
	seen := &types.Commit{
		Height:     height,
		BlockID:    blockID,
		Signatures: []types.CommitSig{types.NewCommitSigAbsent(), types.NewCommitSigAbsent()},
	}
	store.NewBlockStore(blockDB).SaveBlock(block, parts, seen)

	st := sm.State{
		ChainID:                          chainID,
		InitialHeight:                    1,
		LastBlockHeight:                  height,
		LastBlockID:                      blockID,
		LastBlockTime:                    block.Time,
		Validators:                       vals.Copy(),
		NextValidators:                   vals.Copy(),
		LastValidators:                   vals.Copy(),
		LastHeightValidatorsChanged:      1,
		ConsensusParams:                  *types.DefaultConsensusParams(),
		LastHeightConsensusParamsChanged: 1,
		LastResultsHash:                  []byte("last-results-hash"),
		AppHash:                          []byte("app-hash"),
	}
	assert.Nil(t, sm.NewStore(stateDB, sm.StoreOptions{}).Save(st))

	return cometFixture{state: st, blockHash: block.Hash()}
}

func newTestTarget(t *testing.T) *Target {
	t.Helper()
	target, err := OpenTarget(t.TempDir(), "mantlemint")
	assert.Nil(t, err)
	t.Cleanup(func() { _ = target.Close() })
	return target
}

func seedTarget(t *testing.T, target *Target, dataDir string, height int64) error {
	t.Helper()
	src, err := OpenCometState(dataDir)
	assert.Nil(t, err)
	defer src.Close()
	return target.WriteAt(height, func(db dbm.DB) error {
		return src.SeedInto(wrapped.NewWrappedDB(db), height)
	})
}

func TestCometSeedCopiesStateAndBlock(t *testing.T) {
	dir := t.TempDir()
	fx := buildCometDB(t, dir, "rehearsal-1", 50)
	target := newTestTarget(t)

	assert.Nil(t, seedTarget(t, target, dir, 50))

	stateStore := sm.NewStore(wrapped.NewWrappedDB(target.batched), sm.StoreOptions{})
	got, err := stateStore.Load()
	assert.Nil(t, err)
	assert.Equal(t, "rehearsal-1", got.ChainID)
	assert.Equal(t, int64(50), got.LastBlockHeight)
	assert.Equal(t, fx.state.LastBlockID, got.LastBlockID)
	assert.Equal(t, fx.state.AppHash, []byte(got.AppHash))
	assert.Equal(t, fx.state.LastResultsHash, []byte(got.LastResultsHash))
	assert.Equal(t, fx.state.Validators.Hash(), got.Validators.Hash())
	assert.Equal(t, fx.state.NextValidators.Hash(), got.NextValidators.Hash())

	// validator records needed to apply block 51, and params Bootstrap writes at 51
	for _, h := range []int64{50, 51, 52} {
		vals, err := stateStore.LoadValidators(h)
		assert.Nil(t, err, "validators at %d", h)
		assert.Equal(t, fx.state.Validators.Hash(), vals.Hash())
	}
	_, err = stateStore.LoadConsensusParams(51)
	assert.Nil(t, err)

	blocks := store.NewBlockStore(wrapped.NewWrappedDB(target.batched))
	assert.Equal(t, int64(50), blocks.Height())
	block := blocks.LoadBlock(50)
	assert.NotNil(t, block)
	assert.Equal(t, fx.blockHash, []byte(block.Hash()))
	assert.NotNil(t, blocks.LoadSeenCommit(50))
}

// advance simulates what applying blocks does to the state store: each block
// saves the next state, which writes pointer records for later heights.
func advance(t *testing.T, target *Target, from int64, blocks int) sm.Store {
	t.Helper()
	stateStore := sm.NewStore(wrapped.NewWrappedDB(target.batched), sm.StoreOptions{})
	st, err := stateStore.Load()
	assert.Nil(t, err)
	for i := 1; i <= blocks; i++ {
		st.LastBlockHeight = from + int64(i)
		st.LastValidators = st.Validators.Copy()
		assert.Nil(t, target.WriteAt(st.LastBlockHeight, func(db dbm.DB) error {
			return sm.NewStore(wrapped.NewWrappedDB(db), sm.StoreOptions{}).Save(st)
		}))
	}
	return stateStore
}

func TestCometSeedSurvivesBlocksPastImport(t *testing.T) {
	dir := t.TempDir()
	buildCometDB(t, dir, "rehearsal-1", 50)
	target := newTestTarget(t)
	assert.Nil(t, seedTarget(t, target, dir, 50))

	stateStore := advance(t, target, 50, 6)

	// applying block h loads validators at h-1; blocks 51..57 must all resolve
	for h := int64(50); h <= 57; h++ {
		_, err := stateStore.LoadValidators(h)
		assert.Nil(t, err, "validators at %d", h)
	}
	for h := int64(51); h <= 57; h++ {
		_, err := stateStore.LoadConsensusParams(h)
		assert.Nil(t, err, "consensus params at %d", h)
	}
}

// Without rewriting the "last changed" heights, the records written after the
// import point at heights that were never imported. This guards the rewrite.
func TestBootstrapWithoutRewriteBreaksAfterImport(t *testing.T) {
	dir := t.TempDir()
	fx := buildCometDB(t, dir, "rehearsal-1", 50)
	target := newTestTarget(t)

	assert.Nil(t, target.WriteAt(50, func(db dbm.DB) error {
		return sm.NewStore(wrapped.NewWrappedDB(db), sm.StoreOptions{}).Bootstrap(fx.state)
	}))
	stateStore := advance(t, target, 50, 2)

	_, err := stateStore.LoadValidators(53)
	assert.NotNil(t, err)
}

func TestCometSeedRejectsHeightMismatch(t *testing.T) {
	dir := t.TempDir()
	buildCometDB(t, dir, "rehearsal-1", 49)
	target := newTestTarget(t)

	err := seedTarget(t, target, dir, 50)
	assert.ErrorContains(t, err, "CometBFT state is at height 49 but app state is at height 50")

	// nothing was flushed
	loaded, loadErr := sm.NewStore(wrapped.NewWrappedDB(target.batched), sm.StoreOptions{}).Load()
	assert.Nil(t, loadErr)
	assert.True(t, loaded.IsEmpty())
}

func TestCometSeedRejectsEmptyState(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"state", "blockstore"} {
		db, err := dbm.NewGoLevelDB(name, dir)
		assert.Nil(t, err)
		assert.Nil(t, db.Close())
	}

	_, err := OpenCometState(dir)
	assert.ErrorContains(t, err, "no CometBFT state in state.db")
}

func TestCometSeedRejectsVoteExtensions(t *testing.T) {
	dir := t.TempDir()
	fx := buildCometDB(t, dir, "rehearsal-1", 50)

	// re-save the source state with vote extensions enabled
	stateDB, err := dbm.NewGoLevelDB("state", dir)
	assert.Nil(t, err)
	st := fx.state
	st.ConsensusParams.ABCI.VoteExtensionsEnableHeight = 10
	assert.Nil(t, sm.NewStore(stateDB, sm.StoreOptions{}).Save(st))
	assert.Nil(t, stateDB.Close())

	target := newTestTarget(t)
	err = seedTarget(t, target, dir, 50)
	assert.ErrorContains(t, err, "vote extensions are enabled")
}

func TestOpenTargetRefusesNonEmptyDatabase(t *testing.T) {
	dir := t.TempDir()
	target, err := OpenTarget(dir, "mantlemint")
	assert.Nil(t, err)
	assert.Nil(t, target.WriteAt(5, func(db dbm.DB) error {
		return db.Set([]byte("k"), []byte("v"))
	}))
	assert.Nil(t, target.Close())

	_, err = OpenTarget(dir, "mantlemint")
	assert.ErrorContains(t, err, "is not empty")
}
