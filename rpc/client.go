package rpc

import (
	"context"
	"errors"

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
}

var errUnsupportedRPC = errors.New("rpc method is not supported by mantlemint's local ABCI client")

func NewRpcClient(client abcicli.Client) rpcclient.Client {
	return &MantlemintRPCClient{client: client}
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

func (m *MantlemintRPCClient) Block(ctx context.Context, height *int64) (*coretypes.ResultBlock, error) {
	return nil, errUnsupportedRPC
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

func (m *MantlemintRPCClient) Validators(ctx context.Context, height *int64, page, perPage *int) (*coretypes.ResultValidators, error) {
	return nil, errUnsupportedRPC
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

func (m *MantlemintRPCClient) Status(ctx context.Context) (*coretypes.ResultStatus, error) {
	return nil, errUnsupportedRPC
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
