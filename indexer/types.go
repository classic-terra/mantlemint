package indexer

import (
	"log"
	"net/http"
	"runtime"

	dbm "github.com/cometbft/cometbft-db"
	tm "github.com/cometbft/cometbft/types"
	"github.com/gorilla/mux"
	"github.com/terra-money/mantlemint/mantlemint"
)

type (
	IndexFunc           func(indexerDB dbm.Batch, block *tm.Block, blockId *tm.BlockID, evc *mantlemint.EventCollector) error
	ClientHandler       func(w http.ResponseWriter, r *http.Request) error
	RESTRouteRegisterer func(router *mux.Router, indexerDB dbm.DB)
)

func CreateIndexer(idf IndexFunc) IndexFunc {
	return idf
}

func CreateRESTRoute(registerer RESTRouteRegisterer) RESTRouteRegisterer {
	return registerer
}

var ErrorInternal = func(err error) string {
	_, fn, fl, ok := runtime.Caller(1)

	if !ok {
		// ...
	} else {
		log.Printf("ErrorInternal[%s:%d] %v\n", fn, fl, err.Error())
	}

	return "internal server error"
}
