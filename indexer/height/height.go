package height

import (
	"fmt"

	dbm "github.com/cometbft/cometbft-db"
	tmjson "github.com/cometbft/cometbft/libs/json"
	tm "github.com/cometbft/cometbft/types"
	"github.com/terra-money/mantlemint/indexer"
	"github.com/terra-money/mantlemint/mantlemint"
)

var IndexHeight = indexer.CreateIndexer(func(indexerDB dbm.Batch, block *tm.Block, _ *tm.BlockID, _ *mantlemint.EventCollector) error {
	defer fmt.Printf("[indexer/height] indexing done for height %d\n", block.Height)
	height := block.Height

	record := HeightRecord{Height: uint64(height)}
	recordJSON, recordErr := tmjson.Marshal(record)
	if recordErr != nil {
		return recordErr
	}

	return indexerDB.Set(getKey(), recordJSON)
})
