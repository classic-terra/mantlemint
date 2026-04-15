package mantlemint

import (
	abci "github.com/cometbft/cometbft/abci/types"
	tm "github.com/cometbft/cometbft/types"
)

type EventCollector struct {
	Height                int64
	Block                 *tm.Block
	ResponseFinalizeBlock *abci.ResponseFinalizeBlock
	TxResults             []*abci.ExecTxResult
}

func NewMantlemintEventCollector() *EventCollector {
	return &EventCollector{}
}

// PublishEventNewBlock collects block and FinalizeBlock results.
func (ev *EventCollector) PublishEventNewBlock(
	block tm.EventDataNewBlock,
) error {
	ev.Height = block.Block.Height
	ev.Block = block.Block
	ev.ResponseFinalizeBlock = &block.ResultFinalizeBlock

	return nil
}

// PublishEventTx collect txResult in order
func (ev *EventCollector) PublishEventTx(
	txEvent tm.EventDataTx,
) error {
	ev.TxResults = append(ev.TxResults, &txEvent.Result)
	return nil
}

// PublishEventNewBlockEvents is unused.
func (ev *EventCollector) PublishEventNewBlockEvents(
	_ tm.EventDataNewBlockEvents,
) error {
	return nil
}

// PublishEventNewBlockHeader unused
func (ev *EventCollector) PublishEventNewBlockHeader(
	_ tm.EventDataNewBlockHeader,
) error {
	return nil
}

// PublishEventValidatorSetUpdates unused
func (ev *EventCollector) PublishEventValidatorSetUpdates(
	_ tm.EventDataValidatorSetUpdates,
) error {
	return nil
}

// PublishEventNewEvidence unused
func (ev *EventCollector) PublishEventNewEvidence(_ tm.EventDataNewEvidence) error {
	return nil
}
