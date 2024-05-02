package block

import (
	"fmt"
	"io"
	"os"
	"testing"

	dbm "github.com/cometbft/cometbft-db"
	tmjson "github.com/cometbft/cometbft/libs/json"
	"github.com/stretchr/testify/assert"
)

func TestIndexBlock(t *testing.T) {
	db := dbm.NewMemDB()
	blockFile, _ := os.Open("../fixtures/block_4724005_raw.json")
	blockJSON, _ := io.ReadAll(blockFile)

	record := BlockRecord{}
	_ = tmjson.Unmarshal(blockJSON, &record)

	batch := db.NewBatch()
	if err := IndexBlock(batch, record.Block, record.BlockID, nil); err != nil {
		panic(err)
	}
	_ = batch.WriteSync()
	_ = batch.Close()

	block, err := blockByHeightHandler(db, "4724005")
	assert.Nil(t, err)
	assert.NotNil(t, block)

	fmt.Println(string(block))
}
