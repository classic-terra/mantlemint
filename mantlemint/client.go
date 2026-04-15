package mantlemint

import (
	"context"

	abcicli "github.com/cometbft/cometbft/abci/client"
	"github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/libs/service"
	tmsync "github.com/cometbft/cometbft/libs/sync"
)

// NewConcurrentQueryClient creates a local ABCI client that preserves the
// original mantlemint behavior: read-only calls may run concurrently while
// mutating calls remain serialized.
func NewConcurrentQueryClient(mtx *tmsync.RWMutex, app types.Application) abcicli.Client {
	if mtx == nil {
		mtx = &tmsync.RWMutex{}
	}

	cli := &localClient{
		mtx:         mtx,
		Application: app,
	}

	cli.BaseService = *service.NewBaseService(nil, "localClient", cli)
	return cli
}

var _ abcicli.Client = (*localClient)(nil)

type localClient struct {
	service.BaseService

	mtx *tmsync.RWMutex
	types.Application
	abcicli.Callback
}

func (app *localClient) SetResponseCallback(cb abcicli.Callback) {
	app.mtx.Lock()
	defer app.mtx.Unlock()
	app.Callback = cb
}

func (app *localClient) CheckTxAsync(ctx context.Context, req *types.RequestCheckTx) (*abcicli.ReqRes, error) {
	app.mtx.Lock()
	defer app.mtx.Unlock()

	res, err := app.Application.CheckTx(ctx, req)
	if err != nil {
		return nil, err
	}

	return app.callback(
		types.ToRequestCheckTx(req),
		types.ToResponseCheckTx(res),
	), nil
}

func (app *localClient) callback(req *types.Request, res *types.Response) *abcicli.ReqRes {
	app.Callback(req, res)
	reqRes := abcicli.NewReqRes(req)
	reqRes.Response = res
	return reqRes
}

func (app *localClient) Error() error {
	return nil
}

func (app *localClient) Flush(context.Context) error {
	return nil
}

func (app *localClient) Echo(_ context.Context, msg string) (*types.ResponseEcho, error) {
	return &types.ResponseEcho{Message: msg}, nil
}

func (app *localClient) Info(ctx context.Context, req *types.RequestInfo) (*types.ResponseInfo, error) {
	app.mtx.RLock()
	defer app.mtx.RUnlock()

	return app.Application.Info(ctx, req)
}

func (app *localClient) Query(ctx context.Context, req *types.RequestQuery) (*types.ResponseQuery, error) {
	app.mtx.RLock()
	defer app.mtx.RUnlock()

	return app.Application.Query(ctx, req)
}

func (app *localClient) CheckTx(ctx context.Context, req *types.RequestCheckTx) (*types.ResponseCheckTx, error) {
	app.mtx.Lock()
	defer app.mtx.Unlock()

	return app.Application.CheckTx(ctx, req)
}

func (app *localClient) InitChain(ctx context.Context, req *types.RequestInitChain) (*types.ResponseInitChain, error) {
	app.mtx.Lock()
	defer app.mtx.Unlock()

	return app.Application.InitChain(ctx, req)
}

func (app *localClient) PrepareProposal(ctx context.Context, req *types.RequestPrepareProposal) (*types.ResponsePrepareProposal, error) {
	app.mtx.Lock()
	defer app.mtx.Unlock()

	return app.Application.PrepareProposal(ctx, req)
}

func (app *localClient) ProcessProposal(ctx context.Context, req *types.RequestProcessProposal) (*types.ResponseProcessProposal, error) {
	app.mtx.Lock()
	defer app.mtx.Unlock()

	return app.Application.ProcessProposal(ctx, req)
}

func (app *localClient) FinalizeBlock(ctx context.Context, req *types.RequestFinalizeBlock) (*types.ResponseFinalizeBlock, error) {
	app.mtx.Lock()
	defer app.mtx.Unlock()

	return app.Application.FinalizeBlock(ctx, req)
}

func (app *localClient) ExtendVote(ctx context.Context, req *types.RequestExtendVote) (*types.ResponseExtendVote, error) {
	app.mtx.Lock()
	defer app.mtx.Unlock()

	return app.Application.ExtendVote(ctx, req)
}

func (app *localClient) VerifyVoteExtension(ctx context.Context, req *types.RequestVerifyVoteExtension) (*types.ResponseVerifyVoteExtension, error) {
	app.mtx.Lock()
	defer app.mtx.Unlock()

	return app.Application.VerifyVoteExtension(ctx, req)
}

func (app *localClient) Commit(ctx context.Context, req *types.RequestCommit) (*types.ResponseCommit, error) {
	app.mtx.Lock()
	defer app.mtx.Unlock()

	return app.Application.Commit(ctx, req)
}

func (app *localClient) ListSnapshots(ctx context.Context, req *types.RequestListSnapshots) (*types.ResponseListSnapshots, error) {
	app.mtx.Lock()
	defer app.mtx.Unlock()

	return app.Application.ListSnapshots(ctx, req)
}

func (app *localClient) OfferSnapshot(ctx context.Context, req *types.RequestOfferSnapshot) (*types.ResponseOfferSnapshot, error) {
	app.mtx.Lock()
	defer app.mtx.Unlock()

	return app.Application.OfferSnapshot(ctx, req)
}

func (app *localClient) LoadSnapshotChunk(ctx context.Context, req *types.RequestLoadSnapshotChunk) (*types.ResponseLoadSnapshotChunk, error) {
	app.mtx.Lock()
	defer app.mtx.Unlock()

	return app.Application.LoadSnapshotChunk(ctx, req)
}

func (app *localClient) ApplySnapshotChunk(ctx context.Context, req *types.RequestApplySnapshotChunk) (*types.ResponseApplySnapshotChunk, error) {
	app.mtx.Lock()
	defer app.mtx.Unlock()

	return app.Application.ApplySnapshotChunk(ctx, req)
}
