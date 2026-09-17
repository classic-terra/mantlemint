package block

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/pkg/errors"

	dbm "github.com/cometbft/cometbft-db"
	tmjson "github.com/cometbft/cometbft/libs/json"
	tm "github.com/cometbft/cometbft/types"
	"github.com/gorilla/mux"
	"github.com/terra-money/mantlemint/indexer"
)

var EndpointGETBlocksHeight = "/index/blocks/{height}"

var (
	ErrorInvalidHeight = func(height string) string { return fmt.Sprintf("invalid height %s", height) }
	ErrorBlockNotFound = func(height string) string { return fmt.Sprintf("block %s not found... yet.", height) }
)

func blockByHeightHandler(indexerDB dbm.DB, height string) (json.RawMessage, error) {
	heightInInt, err := strconv.Atoi(height)
	if err != nil {
		return nil, errors.New(ErrorInvalidHeight(height))
	}
	return indexerDB.Get(getKey(uint64(heightInInt)))
}

var RegisterRESTRoute = indexer.CreateRESTRoute(func(router *mux.Router, indexerDB dbm.DB) {
	router.HandleFunc(EndpointGETBlocksHeight, func(writer http.ResponseWriter, request *http.Request) {
		vars := mux.Vars(request)
		height, ok := vars["height"]
		if !ok {
			http.Error(writer, ErrorInvalidHeight(height), 400)
			return
		}

		if block, err := blockByHeightHandler(indexerDB, height); err != nil {
			http.Error(writer, indexer.ErrorInternal(err), 500)
			return
		} else if block == nil {
			// block not seen;
			http.Error(writer, ErrorBlockNotFound(height), 400)
			return
		} else {
			writer.WriteHeader(200)
			writer.Write(block)
			return
		}
	}).Methods("GET")
})

// LoadBlock returns the block indexed at height with its block ID, or a nil
// block if that height was never indexed.
func LoadBlock(indexerDB dbm.DB, height int64) (*tm.Block, *tm.BlockID, error) {
	if height <= 0 {
		return nil, nil, errors.New(ErrorInvalidHeight(strconv.FormatInt(height, 10)))
	}
	recordJSON, err := indexerDB.Get(getKey(uint64(height)))
	if err != nil || recordJSON == nil {
		return nil, nil, err
	}
	record := BlockRecord{}
	if err := tmjson.Unmarshal(recordJSON, &record); err != nil {
		return nil, nil, fmt.Errorf("decode indexed block %d: %w", height, err)
	}
	return record.Block, record.BlockID, nil
}
