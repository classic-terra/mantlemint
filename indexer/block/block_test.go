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

func TestLoadBlock(t *testing.T) {
	db := dbm.NewMemDB()
	blockJSON, err := os.ReadFile("../fixtures/block_4724005_raw.json")
	assert.Nil(t, err)
	record := BlockRecord{}
	assert.Nil(t, tmjson.Unmarshal(blockJSON, &record))

	batch := db.NewBatch()
	assert.Nil(t, IndexBlock(batch, record.Block, record.BlockID, nil))
	assert.Nil(t, batch.WriteSync())
	assert.Nil(t, batch.Close())

	block, blockID, err := LoadBlock(db, 4724005)
	assert.Nil(t, err)
	assert.Equal(t, record.Block.Hash(), block.Hash())
	assert.Equal(t, *record.BlockID, *blockID)

	// a height that was never indexed is not an error, just absent
	block, blockID, err = LoadBlock(db, 4724006)
	assert.Nil(t, err)
	assert.Nil(t, block)
	assert.Nil(t, blockID)

	_, _, err = LoadBlock(db, 0)
	assert.ErrorContains(t, err, "invalid height")
}
