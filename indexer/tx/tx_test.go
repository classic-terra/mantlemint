package tx

import (
	"fmt"
	"io"
	"os"
	"testing"

	dbm "github.com/cometbft/cometbft-db"
	tmjson "github.com/cometbft/cometbft/libs/json"
	tendermint "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/assert"
	"github.com/terra-money/mantlemint/mantlemint"
)

func TestIndexTx(t *testing.T) {
	db := dbm.NewMemDB()
	block := &tendermint.Block{}
	blockFile, _ := os.Open("../fixtures/block_4814775.json")
	blockJSON, _ := io.ReadAll(blockFile)
	if err := tmjson.Unmarshal(blockJSON, block); err != nil {
		t.Fail()
	}

	eventFile, _ := os.Open("../fixtures/response_4814775.json")
	eventJSON, _ := io.ReadAll(eventFile)
	evc := mantlemint.NewMantlemintEventCollector()
	event := tendermint.EventDataTx{}
	if err := tmjson.Unmarshal(eventJSON, &event.Result); err != nil {
		panic(err)
	}

	_ = evc.PublishEventTx(event)

	batch := db.NewBatch()
	if err := IndexTx(batch, block, nil, evc); err != nil {
		panic(err)
	}
	_ = batch.WriteSync()
	_ = batch.Close()

	txn, err := txByHashHandler(db, "C794D5CE7179AED455C10E8E7645FE8F8A40BA0C97F1275AB87B5E88A52CB2C3")
	assert.Nil(t, err)
	assert.NotNil(t, txn)
	fmt.Println(string(txn))

	txns, err := txsByHeightHandler(db, "4814775")
	assert.Nil(t, err)
	assert.NotNil(t, txns)
	fmt.Println(string(txns))
}
