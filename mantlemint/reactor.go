package mantlemint

import (
	"log"
	"sync"

	"github.com/cometbft/cometbft/consensus"
	"github.com/cometbft/cometbft/crypto/merkle"
	"github.com/cometbft/cometbft/proxy"
	"github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/store"
	tendermint "github.com/cometbft/cometbft/types"
	"github.com/terra-money/mantlemint/db/wrapped"

	dbm "github.com/cometbft/cometbft-db"
)

var _ Mantlemint = (*Instance)(nil)

type Instance struct {
	executor   Executor
	conn       proxy.AppConns
	db         dbm.DB
	stateStore state.Store
	blockStore state.BlockStore
	mtx        *sync.Mutex

	// mem-cached LastState for faster retrieval
	lastState  state.State
	lastHeight int64
	lastBlock  *tendermint.Block

	evc *EventCollector

	// before and after callback
	runBefore MantlemintCallbackBefore
	runAfter  MantlemintCallbackAfter
}

func NewMantlemint(
	db dbm.DB,
	conn proxy.AppConns,
	executor Executor,
	runBefore MantlemintCallbackBefore,
	runAfter MantlemintCallbackAfter,
) Mantlemint {
	wdb := wrapped.NewWrappedDB(db)
	// here we go!
	stateStore := state.NewStore(wdb, state.StoreOptions{
		DiscardABCIResponses: false,
	})
	blockStore := store.NewBlockStore(wdb)
	lastState, err := stateStore.Load()
	if err != nil {
		panic(err)
	}

	return &Instance{
		// subsystem
		executor:   executor,
		db:         db,
		stateStore: stateStore,
		blockStore: blockStore,
		conn:       conn,
		mtx:        new(sync.Mutex),

		// state related
		lastBlock:  nil,
		lastState:  lastState,
		lastHeight: lastState.LastBlockHeight,
		evc:        nil,

		// mantlemint lifecycle hooks
		runBefore: runBefore,
		runAfter:  runAfter,
	}
}

// Init is port of ReplayBlocks() from tendermint,
// where it only handles initializing the chain.
func (mm *Instance) Init(genesis *tendermint.GenesisDoc) error {
	// loaded state has LastBlockHeight 0,
	// meaning chain was never initialized
	// run genesis
	log.Printf("genesisTime=%v, chainId=%v", genesis.GenesisTime, genesis.ChainID)

	if mm.lastHeight == 0 {
		if genstate, err := state.MakeGenesisState(genesis); err != nil {
			return err
		} else {
			mm.lastState = genstate
		}

		// need a handshaker
		hs := consensus.NewHandshaker(mm.stateStore, mm.lastState, mm.blockStore, genesis)

		var initialAppHash []byte
		if _, err := hs.ReplayBlocks(mm.lastState, initialAppHash, 0, mm.conn); err != nil {
			return err
		}

	}

	return nil
}

func (mm *Instance) LoadInitialState() error {
	if lastState, err := mm.stateStore.Load(); err != nil {
		return err
	} else {
		mm.lastState = lastState
	}

	if mm.lastHeight == 0 {
		mm.lastState.LastResultsHash = merkle.HashFromByteSlices(nil)
	}
	return nil
}

func (mm *Instance) Inject(block *tendermint.Block) error {
	// apply this block
	var nextState state.State
	var retainHeight int64
	var err error

	currentState := mm.lastState
	partset, err := block.MakePartSet(tendermint.BlockPartSizeBytes)
	if err != nil {
		return err
	}
	blockID := tendermint.BlockID{
		Hash:          block.Hash(),
		PartSetHeader: partset.Header(),
	}

	// patch AppHash of lastState to the current block's last app hash
	// because we still want to use fauxMerkleTree for speed (way faster this way!)
	currentState.AppHash = block.Header.AppHash

	// set new event listener for this round
	// note that we create new event collector for every block,
	// however this operation is quite cheap.
	mm.evc = NewMantlemintEventCollector()
	mm.executor.SetEventBus(mm.evc)

	if runBeforeErr := mm.safeRunBefore(block); runBeforeErr != nil {
		return runBeforeErr
	}

	// process blocks
	if nextState, retainHeight, err = mm.executor.ApplyBlock(currentState, blockID, block); err != nil {
		return err
	}

	log.Printf("[mantlemint/inject] nextState.LastBlockHeight=%d, nextState.LastResultsHash=%x", nextState.LastBlockHeight, nextState.LastResultsHash)

	// save cache of last state
	mm.lastBlock = block
	mm.lastState = nextState
	mm.lastHeight = retainHeight

	if runAfterErr := mm.safeRunAfter(block, mm.evc); runAfterErr != nil {
		return runAfterErr
	}

	// read events, form blockState and return it
	return nil
}

func (mm *Instance) GetCurrentHeight() int64 {
	if mm.lastState.LastBlockHeight != 0 {
		return mm.lastState.LastBlockHeight
	} else {
		return mm.lastState.InitialHeight - 1
	}
}

func (mm *Instance) GetCurrentBlock() *tendermint.Block {
	return mm.lastBlock
}

func (mm *Instance) GetCurrentState() state.State {
	return mm.lastState
}

func (mm *Instance) SetBlockExecutor(nextBlockExecutor Executor) {
	mm.executor = nextBlockExecutor
}

func (mm *Instance) GetCurrentEventCollector() *EventCollector {
	return mm.evc
}

func (mm *Instance) safeRunBefore(block *tendermint.Block) error {
	if mm.runBefore != nil {
		return mm.runBefore(block)
	} else {
		return nil
	}
}

func (mm *Instance) safeRunAfter(block *tendermint.Block, events *EventCollector) error {
	if mm.runBefore != nil {
		return mm.runAfter(block, events)
	} else {
		return nil
	}
}
