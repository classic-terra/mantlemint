package rpc

import (
	"context"
	"errors"
	"fmt"

	abcicli "github.com/cometbft/cometbft/abci/client"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/libs/bytes"
	tmlog "github.com/cometbft/cometbft/libs/log"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	tendermint "github.com/cometbft/cometbft/types"
)

var _ rpcclient.Client = (*MantlemintRPCClient)(nil)

type MantlemintRPCClient struct {
	client abcicli.Client
	chain  ChainData
}

var errUnsupportedRPC = errors.New("rpc method is not supported by mantlemint's local ABCI client")

func NewRpcClient(client abcicli.Client, chain ChainData) rpcclient.Client {
	return &MantlemintRPCClient{client: client, chain: chain}
}

func (m *MantlemintRPCClient) ABCIInfo(ctx context.Context) (*coretypes.ResultABCIInfo, error) {
	resp, err := m.client.Info(ctx, &abci.RequestInfo{})
	if err != nil {
		return nil, err
	}

	return &coretypes.ResultABCIInfo{Response: *resp}, nil
}

func (m *MantlemintRPCClient) ABCIQuery(ctx context.Context, path string, data bytes.HexBytes) (*coretypes.ResultABCIQuery, error) {
	if resp, err := m.client.Query(ctx, &abci.RequestQuery{
		Data:   data,
		Path:   path,
		Height: 0,
		Prove:  false,
	}); err != nil {
		return nil, err
	} else {
		return &coretypes.ResultABCIQuery{
			Response: *resp,
		}, nil
	}
}

func (m *MantlemintRPCClient) ABCIQueryWithOptions(ctx context.Context, path string, data bytes.HexBytes, opts rpcclient.ABCIQueryOptions) (*coretypes.ResultABCIQuery, error) {
	if resp, err := m.client.Query(ctx, &abci.RequestQuery{
		Data:   data,
		Path:   path,
		Height: opts.Height,
		Prove:  opts.Prove,
	}); err != nil {
		return nil, err
	} else {
		return &coretypes.ResultABCIQuery{
			Response: *resp,
		}, nil
	}
}

func (m *MantlemintRPCClient) Start() error {
	return m.client.Start()
}

func (m *MantlemintRPCClient) OnStart() error {
	return m.client.OnStart()
}

func (m *MantlemintRPCClient) Stop() error {
	return m.client.Stop()
}

func (m *MantlemintRPCClient) OnStop() {
	m.client.OnStop()
}

func (m *MantlemintRPCClient) Reset() error {
	return m.client.Reset()
}

func (m *MantlemintRPCClient) OnReset() error {
	return m.client.OnReset()
}

func (m *MantlemintRPCClient) IsRunning() bool {
	return m.client.IsRunning()
}

func (m *MantlemintRPCClient) Quit() <-chan struct{} {
	return m.client.Quit()
}

func (m *MantlemintRPCClient) String() string {
	return m.client.String()
}

func (m *MantlemintRPCClient) SetLogger(logger tmlog.Logger) {
	m.client.SetLogger(logger)
}

func (m *MantlemintRPCClient) Header(ctx context.Context, height *int64) (*coretypes.ResultHeader, error) {
	return nil, errUnsupportedRPC
}

func (m *MantlemintRPCClient) HeaderByHash(ctx context.Context, hash bytes.HexBytes) (*coretypes.ResultHeader, error) {
	return nil, errUnsupportedRPC
}

func (m *MantlemintRPCClient) BroadcastTxCommit(ctx context.Context, tx tendermint.Tx) (*coretypes.ResultBroadcastTxCommit, error) {
	return nil, errUnsupportedRPC
}

func (m *MantlemintRPCClient) BroadcastTxAsync(ctx context.Context, tx tendermint.Tx) (*coretypes.ResultBroadcastTx, error) {
	return nil, errUnsupportedRPC
}

func (m *MantlemintRPCClient) BroadcastTxSync(ctx context.Context, tx tendermint.Tx) (*coretypes.ResultBroadcastTx, error) {
	return nil, errUnsupportedRPC
}

func (m *MantlemintRPCClient) Subscribe(ctx context.Context, subscriber, query string, outCapacity ...int) (out <-chan coretypes.ResultEvent, err error) {
	return nil, errUnsupportedRPC
}

func (m *MantlemintRPCClient) Unsubscribe(ctx context.Context, subscriber, query string) error {
	return errUnsupportedRPC
}

func (m *MantlemintRPCClient) UnsubscribeAll(ctx context.Context, subscriber string) error {
	return errUnsupportedRPC
}

func (m *MantlemintRPCClient) Genesis(ctx context.Context) (*coretypes.ResultGenesis, error) {
	return nil, errUnsupportedRPC
}

func (m *MantlemintRPCClient) GenesisChunked(ctx context.Context, u uint) (*coretypes.ResultGenesisChunk, error) {
	return nil, errUnsupportedRPC
}

func (m *MantlemintRPCClient) BlockchainInfo(ctx context.Context, minHeight, maxHeight int64) (*coretypes.ResultBlockchainInfo, error) {
	return nil, errUnsupportedRPC
}

func (m *MantlemintRPCClient) NetInfo(ctx context.Context) (*coretypes.ResultNetInfo, error) {
	return nil, errUnsupportedRPC
}

func (m *MantlemintRPCClient) DumpConsensusState(ctx context.Context) (*coretypes.ResultDumpConsensusState, error) {
	return nil, errUnsupportedRPC
}

func (m *MantlemintRPCClient) ConsensusState(ctx context.Context) (*coretypes.ResultConsensusState, error) {
	return nil, errUnsupportedRPC
}

func (m *MantlemintRPCClient) ConsensusParams(ctx context.Context, height *int64) (*coretypes.ResultConsensusParams, error) {
	return nil, errUnsupportedRPC
}

func (m *MantlemintRPCClient) Health(ctx context.Context) (*coretypes.ResultHealth, error) {
	return &coretypes.ResultHealth{}, nil
}

func (m *MantlemintRPCClient) Block(ctx context.Context, heightPtr *int64) (*coretypes.ResultBlock, error) {
	height, err := resolveHeight(m.chain.LatestHeight(), heightPtr)
	if err != nil {
		return nil, err
	}
	block, blockID, err := m.chain.Block(height)
	if err != nil {
		return nil, err
	}
	if block == nil {
		return nil, fmt.Errorf("block at height %d is not available", height)
	}
	return &coretypes.ResultBlock{BlockID: *blockID, Block: block}, nil
}

func (m *MantlemintRPCClient) BlockByHash(ctx context.Context, hash []byte) (*coretypes.ResultBlock, error) {
	return nil, errUnsupportedRPC
}

func (m *MantlemintRPCClient) BlockResults(ctx context.Context, height *int64) (*coretypes.ResultBlockResults, error) {
	return nil, errUnsupportedRPC
}

func (m *MantlemintRPCClient) Commit(ctx context.Context, height *int64) (*coretypes.ResultCommit, error) {
	return nil, errUnsupportedRPC
}

func (m *MantlemintRPCClient) Validators(ctx context.Context, heightPtr *int64, pagePtr, perPagePtr *int) (*coretypes.ResultValidators, error) {
	// as in CometBFT, a synced node's latest validator set is the next height's
	latest := m.chain.LatestHeight()
	if m.chain.IsSynced() {
		latest++
	}
	height, err := resolveHeight(latest, heightPtr)
	if err != nil {
		return nil, err
	}
	validators, err := m.chain.Validators(height)
	if err != nil {
		return nil, err
	}

	total := len(validators.Validators)
	perPage := validatePerPage(perPagePtr)
	page, err := validatePage(pagePtr, perPage, total)
	if err != nil {
		return nil, err
	}
	skip := (page - 1) * perPage
	v := validators.Validators[skip : skip+min(perPage, total-skip)]

	return &coretypes.ResultValidators{BlockHeight: height, Validators: v, Count: len(v), Total: total}, nil
}

func (m *MantlemintRPCClient) Tx(ctx context.Context, hash []byte, prove bool) (*coretypes.ResultTx, error) {
	return nil, errUnsupportedRPC
}

func (m *MantlemintRPCClient) TxSearch(ctx context.Context, query string, prove bool, page, perPage *int, orderBy string) (*coretypes.ResultTxSearch, error) {
	return nil, errUnsupportedRPC
}

func (m *MantlemintRPCClient) BlockSearch(ctx context.Context, query string, page, perPage *int, orderBy string) (*coretypes.ResultBlockSearch, error) {
	return nil, errUnsupportedRPC
}

// Status reports sync information only; mantlemint is not a p2p node, so node
// and validator info stay empty.
func (m *MantlemintRPCClient) Status(ctx context.Context) (*coretypes.ResultStatus, error) {
	height := m.chain.LatestHeight()
	syncInfo := coretypes.SyncInfo{LatestBlockHeight: height, CatchingUp: !m.chain.IsSynced()}
	block, _, err := m.chain.Block(height)
	if err != nil {
		return nil, err
	}
	if block != nil {
		syncInfo.LatestBlockHash = block.Hash()
		syncInfo.LatestAppHash = block.AppHash
		syncInfo.LatestBlockTime = block.Time
	}
	return &coretypes.ResultStatus{SyncInfo: syncInfo}, nil
}

func (m *MantlemintRPCClient) BroadcastEvidence(ctx context.Context, evidence tendermint.Evidence) (*coretypes.ResultBroadcastEvidence, error) {
	return nil, errUnsupportedRPC
}

func (m *MantlemintRPCClient) UnconfirmedTxs(ctx context.Context, limit *int) (*coretypes.ResultUnconfirmedTxs, error) {
	return nil, errUnsupportedRPC
}

func (m *MantlemintRPCClient) NumUnconfirmedTxs(ctx context.Context) (*coretypes.ResultUnconfirmedTxs, error) {
	return nil, errUnsupportedRPC
}

// The helpers below follow CometBFT's rpc/core so results match a full node.
const (
	defaultPerPage = 30
	maxPerPage     = 100
)

func resolveHeight(latest int64, heightPtr *int64) (int64, error) {
	if heightPtr == nil {
		return latest, nil
	}
	height := *heightPtr
	if height <= 0 {
		return 0, fmt.Errorf("height must be greater than 0, but got %d", height)
	}
	if height > latest {
		return 0, fmt.Errorf("height %d must be less than or equal to the current blockchain height %d", height, latest)
	}
	return height, nil
}

func validatePerPage(perPagePtr *int) int {
	if perPagePtr == nil || *perPagePtr < 1 {
		return defaultPerPage
	}
	return min(*perPagePtr, maxPerPage)
}

func validatePage(pagePtr *int, perPage, total int) (int, error) {
	if pagePtr == nil {
		return 1, nil
	}
	pages := max((total-1)/perPage+1, 1)
	if page := *pagePtr; page <= 0 || page > pages {
		return 1, fmt.Errorf("page should be within [1, %d] range, given %d", pages, page)
	}
	return *pagePtr, nil
}

func (m *MantlemintRPCClient) CheckTx(ctx context.Context, tx tendermint.Tx) (*coretypes.ResultCheckTx, error) {
	resp, err := m.client.CheckTx(ctx, &abci.RequestCheckTx{
		Tx:   tx,
		Type: abci.CheckTxType_New,
	})
	if err != nil {
		return nil, err
	}

	return &coretypes.ResultCheckTx{ResponseCheckTx: *resp}, nil
}
