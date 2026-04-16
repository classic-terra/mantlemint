package mantlemint

import (
	"context"

	abci "github.com/cometbft/cometbft/abci/types"
)

type legacyABCIApplication interface {
	Info(*abci.RequestInfo) (*abci.ResponseInfo, error)
	Query(context.Context, *abci.RequestQuery) (*abci.ResponseQuery, error)
	CheckTx(*abci.RequestCheckTx) (*abci.ResponseCheckTx, error)
	InitChain(*abci.RequestInitChain) (*abci.ResponseInitChain, error)
	PrepareProposal(*abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error)
	ProcessProposal(*abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error)
	FinalizeBlock(*abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error)
	ExtendVote(context.Context, *abci.RequestExtendVote) (*abci.ResponseExtendVote, error)
	VerifyVoteExtension(*abci.RequestVerifyVoteExtension) (*abci.ResponseVerifyVoteExtension, error)
	Commit() (*abci.ResponseCommit, error)
	ListSnapshots(*abci.RequestListSnapshots) (*abci.ResponseListSnapshots, error)
	OfferSnapshot(*abci.RequestOfferSnapshot) (*abci.ResponseOfferSnapshot, error)
	LoadSnapshotChunk(*abci.RequestLoadSnapshotChunk) (*abci.ResponseLoadSnapshotChunk, error)
	ApplySnapshotChunk(*abci.RequestApplySnapshotChunk) (*abci.ResponseApplySnapshotChunk, error)
}

type legacyABCIAdapter struct {
	app legacyABCIApplication
}

var _ abci.Application = legacyABCIAdapter{}

func WrapLegacyABCIApplication(app legacyABCIApplication) abci.Application {
	return legacyABCIAdapter{app: app}
}

func (a legacyABCIAdapter) Info(_ context.Context, req *abci.RequestInfo) (*abci.ResponseInfo, error) {
	return a.app.Info(req)
}

func (a legacyABCIAdapter) Query(ctx context.Context, req *abci.RequestQuery) (*abci.ResponseQuery, error) {
	return a.app.Query(ctx, req)
}

func (a legacyABCIAdapter) CheckTx(_ context.Context, req *abci.RequestCheckTx) (*abci.ResponseCheckTx, error) {
	return a.app.CheckTx(req)
}

func (a legacyABCIAdapter) InitChain(_ context.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	return a.app.InitChain(req)
}

func (a legacyABCIAdapter) PrepareProposal(_ context.Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
	return a.app.PrepareProposal(req)
}

func (a legacyABCIAdapter) ProcessProposal(_ context.Context, req *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
	return a.app.ProcessProposal(req)
}

func (a legacyABCIAdapter) FinalizeBlock(_ context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	return a.app.FinalizeBlock(req)
}

func (a legacyABCIAdapter) ExtendVote(ctx context.Context, req *abci.RequestExtendVote) (*abci.ResponseExtendVote, error) {
	return a.app.ExtendVote(ctx, req)
}

func (a legacyABCIAdapter) VerifyVoteExtension(_ context.Context, req *abci.RequestVerifyVoteExtension) (*abci.ResponseVerifyVoteExtension, error) {
	return a.app.VerifyVoteExtension(req)
}

func (a legacyABCIAdapter) Commit(_ context.Context, _ *abci.RequestCommit) (*abci.ResponseCommit, error) {
	return a.app.Commit()
}

func (a legacyABCIAdapter) ListSnapshots(_ context.Context, req *abci.RequestListSnapshots) (*abci.ResponseListSnapshots, error) {
	return a.app.ListSnapshots(req)
}

func (a legacyABCIAdapter) OfferSnapshot(_ context.Context, req *abci.RequestOfferSnapshot) (*abci.ResponseOfferSnapshot, error) {
	return a.app.OfferSnapshot(req)
}

func (a legacyABCIAdapter) LoadSnapshotChunk(_ context.Context, req *abci.RequestLoadSnapshotChunk) (*abci.ResponseLoadSnapshotChunk, error) {
	return a.app.LoadSnapshotChunk(req)
}

func (a legacyABCIAdapter) ApplySnapshotChunk(_ context.Context, req *abci.RequestApplySnapshotChunk) (*abci.ResponseApplySnapshotChunk, error) {
	return a.app.ApplySnapshotChunk(req)
}
