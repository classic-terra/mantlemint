package mantlemint

import (
	"os"

	"github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/mempool/mocks"
	"github.com/cometbft/cometbft/proxy"
	"github.com/cometbft/cometbft/state"
	"github.com/terra-money/mantlemint/db/wrapped"

	dbm "github.com/cometbft/cometbft-db"
)

// NewMantlemintExecutor creates stock tendermint block executor, with stubbed mempool and evidence pool
func NewMantlemintExecutor(
	db dbm.DB,
	conn proxy.AppConnConsensus,
) *state.BlockExecutor {
	return state.NewBlockExecutor(
		state.NewStore(wrapped.NewWrappedDB(db), state.StoreOptions{
			DiscardABCIResponses: false,
		}),

		// discard all tm logging
		log.NewTMLogger(os.Stdout),

		// use app connection as provided
		conn,

		// no mempool, as mantlemint doesn't handle tx broadcasts
		mocks.Mempool{},

		// no evidence pool, as mantlemint only receives evidence from other peers
		state.EmptyEvidencePool{},
	)
}
