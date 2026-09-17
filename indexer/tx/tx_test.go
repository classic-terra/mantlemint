package tx

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"

	dbm "github.com/cometbft/cometbft-db"
	abci "github.com/cometbft/cometbft/abci/types"
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

	hash, err := hex.DecodeString("C794D5CE7179AED455C10E8E7645FE8F8A40BA0C97F1275AB87B5E88A52CB2C3")
	assert.Nil(t, err)
	height, index, found, err := LoadTxLocation(db, hash)
	assert.Nil(t, err)
	assert.True(t, found)
	assert.Equal(t, int64(4814775), height)
	assert.Equal(t, block.Txs[index].Hash(), []byte(hash))

	_, _, found, err = LoadTxLocation(db, make([]byte, 32))
	assert.Nil(t, err)
	assert.False(t, found)
}

// TestTxLogsMatchPublicNode rebuilds the logs of real columbus-5 txs from their
// results and compares them with what publicnode's mantlemint serves at
// /index/tx/by_height for the same blocks.
func TestTxLogsMatchPublicNode(t *testing.T) {
	for _, height := range []string{"30435740", "30438000", "30438001", "30438019"} {
		var results []abci.ExecTxResult
		readFixture(t, "../fixtures/logs/results_"+height+".json", &results)
		var expected []json.RawMessage
		readFixture(t, "../fixtures/logs/logs_"+height+".json", &expected)

		assert.Len(t, results, len(expected), height)
		for i := range results {
			assert.JSONEq(t, string(expected[i]), string(txLogs(&results[i])), "height %s tx %d", height, i)
		}
	}
}

func TestTxLogs(t *testing.T) {
	event := func(typ string, attrs ...string) abci.Event {
		e := abci.Event{Type: typ}
		for i := 0; i < len(attrs); i += 2 {
			e.Attributes = append(e.Attributes, abci.EventAttribute{Key: attrs[i], Value: attrs[i+1]})
		}
		return e
	}

	// pre-0.50 logs are kept as they are
	legacy := `[{"msg_index":0,"log":"","events":[]}]`
	assert.Equal(t, legacy, string(txLogs(&abci.ExecTxResult{Log: legacy})))

	// failed txs have no message logs
	assert.Equal(t, `[]`, string(txLogs(&abci.ExecTxResult{Code: 5, Log: "out of gas"})))

	// no message events at all
	assert.Equal(t, `[]`, string(txLogs(&abci.ExecTxResult{Events: []abci.Event{event("tx", "fee", "1uluna")}})))

	// grouped by msg_index in message order, even when events arrive out of order
	got := txLogs(&abci.ExecTxResult{Events: []abci.Event{
		event("tx", "fee", "1uluna"),
		event("message", "action", "b", "msg_index", "1"),
		event("message", "action", "a", "msg_index", "0"),
		event("transfer", "amount", "1uluna", "msg_index", "0"),
	}})
	assert.JSONEq(t, `[
		{"msg_index":0,"log":"","events":[
			{"type":"message","attributes":[{"key":"action","value":"a"}]},
			{"type":"transfer","attributes":[{"key":"amount","value":"1uluna"}]}]},
		{"msg_index":1,"log":"","events":[
			{"type":"message","attributes":[{"key":"action","value":"b"}]}]}
	]`, string(got))

	// every result must survive marshalling into the by-height record
	_, err := tmjson.Marshal(TxByHeightRecord{Logs: got})
	assert.Nil(t, err)
}

func readFixture(t *testing.T, path string, v any) {
	t.Helper()
	bz, err := os.ReadFile(path)
	assert.Nil(t, err)
	assert.Nil(t, tmjson.Unmarshal(bz, v))
}
