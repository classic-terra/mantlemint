package tx

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	terra "github.com/classic-terra/core/v4/app"
	dbm "github.com/cometbft/cometbft-db"
	abci "github.com/cometbft/cometbft/abci/types"
	tmjson "github.com/cometbft/cometbft/libs/json"
	tm "github.com/cometbft/cometbft/types"
	"github.com/terra-money/mantlemint/indexer"
	"github.com/terra-money/mantlemint/mantlemint"
)

var cdc = terra.MakeEncodingConfig()

var IndexTx = indexer.CreateIndexer(func(batch dbm.Batch, block *tm.Block, blockID *tm.BlockID, evc *mantlemint.EventCollector) error {
	// encoder; proto -> mem -> json
	txDecoder := cdc.TxConfig.TxDecoder()
	jsonEncoder := cdc.TxConfig.TxJSONEncoder()

	txHashes := make([]string, len(block.Txs))
	txRecords := make([]TxRecord, len(block.Txs))
	byHeightPayload := make([]TxByHeightRecord, len(block.Txs))

	// by hash
	for txIndex, txByte := range block.Txs {
		txRecord := TxRecord{}

		hash := txByte.Hash()
		tx, decodeErr := txDecoder(txByte)

		if decodeErr != nil {
			return decodeErr
		}

		// encode tx to JSON for max compat & shave deserialization cost at serving
		txJSON, _ := jsonEncoder(tx)

		// handle response -> json
		response := ToResponseDeliverTxJSON(evc.TxResults[txIndex])
		responseJSON, responseMarshalErr := tmjson.Marshal(response)

		if responseMarshalErr != nil {
			return responseMarshalErr
		}

		// populate txRecord
		txRecord.Tx = txJSON
		txRecord.TxResponse = responseJSON

		txHashes[txIndex] = fmt.Sprintf("%X", hash)
		txRecords[txIndex] = txRecord

		// byHeightRecord
		// handle non-successful case first
		byHeightPayload[txIndex].Code = response.Code
		byHeightPayload[txIndex].Codespace = response.Codespace
		byHeightPayload[txIndex].GasUsed = response.GasUsed
		byHeightPayload[txIndex].GasWanted = response.GasWanted
		byHeightPayload[txIndex].Height = block.Height
		byHeightPayload[txIndex].RawLog = response.Log
		byHeightPayload[txIndex].Logs = txLogs(evc.TxResults[txIndex])
		byHeightPayload[txIndex].TxHash = fmt.Sprintf("%X", hash)
		byHeightPayload[txIndex].Timestamp = block.Time
		byHeightPayload[txIndex].Tx = txJSON
	}

	// 1. byHash -- matching the interface for /cosmos/tx/v1beta1/txs/{hash}
	for txIndex, txRecord := range txRecords {
		txRecordJSON, marshalErr := tmjson.Marshal(txRecord)
		if marshalErr != nil {
			return marshalErr
		}

		batchSetErr := batch.Set(getKey(txHashes[txIndex]), txRecordJSON)
		if batchSetErr != nil {
			return batchSetErr
		}
		if err := batch.Set(getLocationKey(txHashes[txIndex]), encodeTxLocation(block.Height, uint32(txIndex))); err != nil {
			return err
		}
	}

	// 2. byHeight -- custom endpoint
	byHeightJSON, byHeightErr := tmjson.Marshal(byHeightPayload)
	if byHeightErr != nil {
		return byHeightErr
	}

	batchSetErr := batch.Set(getByHeightKey(uint64(block.Height)), byHeightJSON)
	if batchSetErr != nil {
		return batchSetErr
	}

	return nil
})

// messageLog is one entry of the legacy per-message tx logs.
type messageLog struct {
	MsgIndex uint32     `json:"msg_index"`
	Log      string     `json:"log"`
	Events   []logEvent `json:"events"`
}

type logEvent struct {
	Type       string         `json:"type"`
	Attributes []logAttribute `json:"attributes"`
}

type logAttribute struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

const msgIndexKey = "msg_index"

// txLogs returns the per-message logs of a successful tx. Before cosmos-sdk
// 0.50 the tx log already held them as JSON. Since then the log is empty, and
// the logs are rebuilt from the events the way other classic nodes serve them:
// events carrying a msg_index attribute are grouped by it, in message order,
// without that attribute; tx-level events, which have no msg_index, are left out.
func txLogs(result *abci.ExecTxResult) json.RawMessage {
	if result.Code != 0 {
		return json.RawMessage("[]")
	}
	if result.Log != "" && json.Valid([]byte(result.Log)) {
		return json.RawMessage(result.Log)
	}

	logs := []messageLog{}
	byIndex := map[uint32]int{}
	for _, event := range result.Events {
		index, ok := eventMsgIndex(event)
		if !ok {
			continue
		}
		pos, seen := byIndex[index]
		if !seen {
			pos = len(logs)
			byIndex[index] = pos
			logs = append(logs, messageLog{MsgIndex: index, Events: []logEvent{}})
		}
		logEvt := logEvent{Type: event.Type, Attributes: []logAttribute{}}
		for _, attr := range event.Attributes {
			if attr.Key != msgIndexKey {
				logEvt.Attributes = append(logEvt.Attributes, logAttribute{Key: attr.Key, Value: attr.Value})
			}
		}
		logs[pos].Events = append(logs[pos].Events, logEvt)
	}
	sort.SliceStable(logs, func(i, j int) bool { return logs[i].MsgIndex < logs[j].MsgIndex })

	out, err := json.Marshal(logs)
	if err != nil {
		return json.RawMessage("[]")
	}
	return out
}

func eventMsgIndex(event abci.Event) (uint32, bool) {
	for _, attr := range event.Attributes {
		if attr.Key == msgIndexKey {
			index, err := strconv.ParseUint(attr.Value, 10, 32)
			return uint32(index), err == nil
		}
	}
	return 0, false
}

// LoadTxLocation returns the height and in-block index of the tx with the
// given hash. Txs indexed before locations were recorded are not found.
func LoadTxLocation(indexerDB dbm.DB, hash []byte) (height int64, index uint32, found bool, err error) {
	bz, err := indexerDB.Get(getLocationKey(fmt.Sprintf("%X", hash)))
	if err != nil || bz == nil {
		return 0, 0, false, err
	}
	if len(bz) != 12 {
		return 0, 0, false, fmt.Errorf("tx %X: malformed location record", hash)
	}
	return int64(binary.BigEndian.Uint64(bz[:8])), binary.BigEndian.Uint32(bz[8:]), true, nil
}

func encodeTxLocation(height int64, index uint32) []byte {
	bz := make([]byte, 12)
	binary.BigEndian.PutUint64(bz[:8], uint64(height))
	binary.BigEndian.PutUint32(bz[8:], index)
	return bz
}
