package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"runtime/debug"

	"github.com/CosmWasm/wasmd/x/wasm"
	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	terra "github.com/classic-terra/core/v3/app"
	core "github.com/classic-terra/core/v3/types"
	tmlog "github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/proxy"
	tendermint "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gorilla/mux"
	blockFeeder "github.com/terra-money/mantlemint/block_feed"
	"github.com/terra-money/mantlemint/config"
	"github.com/terra-money/mantlemint/db/heleveldb"
	"github.com/terra-money/mantlemint/db/hld"
	"github.com/terra-money/mantlemint/db/safe_batch"
	"github.com/terra-money/mantlemint/indexer"
	"github.com/terra-money/mantlemint/indexer/block"
	"github.com/terra-money/mantlemint/indexer/tx"
	"github.com/terra-money/mantlemint/mantlemint"
	"github.com/terra-money/mantlemint/rpc"
	"github.com/terra-money/mantlemint/store/rootmulti"

	dbm "github.com/cometbft/cometbft-db"
)

// initialize mantlemint for v0.34.x
func main() {
	mantlemintConfig := config.NewConfig()
	mantlemintConfig.Print()

	sdkConfig := sdk.GetConfig()
	sdkConfig.SetCoinType(core.CoinType)
	sdkConfig.SetBech32PrefixForAccount(core.Bech32PrefixAccAddr, core.Bech32PrefixAccPub)
	sdkConfig.SetBech32PrefixForValidator(core.Bech32PrefixValAddr, core.Bech32PrefixValPub)
	sdkConfig.SetBech32PrefixForConsensusNode(core.Bech32PrefixConsAddr, core.Bech32PrefixConsPub)
	sdkConfig.SetAddressVerifier(wasmtypes.VerifyAddressLen())
	sdkConfig.Seal()

	ldb, ldbErr := heleveldb.NewLevelDBDriver(&heleveldb.DriverConfig{
		Name: mantlemintConfig.MantlemintDB,
		Dir:  mantlemintConfig.Home,
		Mode: heleveldb.DriverModeKeySuffixDesc,
	})
	if ldbErr != nil {
		panic(ldbErr)
	}

	hldb := hld.ApplyHeightLimitedDB(
		ldb,
		&hld.HeightLimitedDBConfig{
			Debug: true,
		},
	)

	batched := safe_batch.NewSafeBatchDB(hldb)
	batchedOrigin := batched.(safe_batch.SafeBatchDBCloser)
	logger := tmlog.NewTMLogger(os.Stdout)
	codec := terra.MakeEncodingConfig()

	// customize CMS to limit kv store's read height on query
	cms := rootmulti.NewStore(batched, logger, hldb)
	//vpr := viper.GetViper()
	appOptions := make(simtestutil.AppOptionsMap, 0)

	var wasmOpts []wasm.Option
	app := terra.NewTerraApp(
		logger,
		batched,
		nil,
		true, // need this so KVStores are set
		make(map[int64]bool),
		mantlemintConfig.Home,
		codec,
		appOptions,
		wasmOpts,
		fauxMerkleModeOpt,
		func(ba *baseapp.BaseApp) {
			ba.SetCMS(cms)
		},
		baseapp.SetChainID(mantlemintConfig.ChainID),
	)

	// create app...
	appCreator := mantlemint.NewConcurrentQueryClientCreator(app)
	appMetrics := proxy.NopMetrics()
	appConns := proxy.NewAppConns(appCreator, appMetrics)
	appConns.SetLogger(logger)
	if startErr := appConns.OnStart(); startErr != nil {
		panic(startErr)
	}

	go func() {
		a := <-appConns.Quit()
		fmt.Println(a)
	}()

	executor := mantlemint.NewMantlemintExecutor(batched, appConns.Consensus())
	mm := mantlemint.NewMantlemint(
		batched,
		appConns,
		executor,

		// run before
		nil,

		// RunAfter Inject callback
		nil,
	)

	// initialize using provided genesis
	genesisDoc := getGenesisDoc(mantlemintConfig.GenesisPath)
	initialHeight := genesisDoc.InitialHeight

	// set target initial write height to genesis.initialHeight;
	// this is safe as upon Inject it will be set with block.Height
	hldb.SetWriteHeight(initialHeight)
	batchedOrigin.Open()

	// initialize state machine with genesis
	if initErr := mm.Init(genesisDoc); initErr != nil {
		panic(initErr)
	}

	// flush to db; panic upon error (can't proceed)
	if rollback, flushErr := batchedOrigin.Flush(); flushErr != nil {
		debug.PrintStack()
		panic(flushErr)
	} else if rollback != nil {
		rollback.Close()
	}

	// load initial state to mantlemint
	if loadErr := mm.LoadInitialState(); loadErr != nil {
		panic(loadErr)
	}

	// initialization is done; clear write height
	hldb.ClearWriteHeight()

	// get blocks over some sort of transport, inject to mantlemint
	blockFeed := blockFeeder.NewAggregateBlockFeed(
		mm.GetCurrentHeight(),
		mantlemintConfig.RPCEndpoints,
		mantlemintConfig.WSEndpoints,
	)

	// create indexer service
	indexerInstance, indexerInstanceErr := indexer.NewIndexer(mantlemintConfig.IndexerDB, mantlemintConfig.Home)
	if indexerInstanceErr != nil {
		panic(indexerInstanceErr)
	}

	indexerInstance.RegisterIndexerService("tx", tx.IndexTx)
	indexerInstance.RegisterIndexerService("block", block.IndexBlock)

	abcicli, _ := appCreator.NewABCIClient()
	rpccli := rpc.NewRpcClient(abcicli)

	// rest cache invalidate channel
	cacheInvalidateChan := make(chan int64)

	// start RPC server
	rpcErr := rpc.StartRPC(
		app,
		rpccli,
		mantlemintConfig.ChainID,
		codec,
		cacheInvalidateChan,

		// callback for registering custom routers; primarily for indexers
		// default: noop,
		// todo: make this part injectable
		func(router *mux.Router) {
			indexerInstance.RegisterRESTRoute(router, tx.RegisterRESTRoute)
			indexerInstance.RegisterRESTRoute(router, block.RegisterRESTRoute)
		},

		// inject flag checker for synced
		blockFeed.IsSynced,
	)

	if rpcErr != nil {
		panic(rpcErr)
	}

	// start subscribing to block
	if mantlemintConfig.DisableSync {
		fmt.Println("running without sync...")
		forever()
	} else if cBlockFeed, blockFeedErr := blockFeed.Subscribe(0); blockFeedErr != nil {
		panic(blockFeedErr)
	} else {
		var rollbackBatch dbm.Batch
		for {
			feed := <-cBlockFeed

			// open db batch
			hldb.SetWriteHeight(feed.Block.Height)
			batchedOrigin.Open()
			if injectErr := mm.Inject(feed.Block); injectErr != nil {
				// rollback last block
				if rollbackBatch != nil {
					fmt.Println("rollback previous block")
					rollbackBatch.WriteSync()
					rollbackBatch.Close()
				}

				debug.PrintStack()
				panic(injectErr)
			}

			// last block is okay -> dispose rollback batch
			if rollbackBatch != nil {
				rollbackBatch.Close()
				rollbackBatch = nil
			}

			// run indexer BEFORE batch flush
			if indexerErr := indexerInstance.Run(feed.Block, feed.BlockID, mm.GetCurrentEventCollector()); indexerErr != nil {
				debug.PrintStack()
				panic(indexerErr)
			}

			// flush db batch
			// returns rollback batch that reverts current block injection
			if rollback, flushErr := batchedOrigin.Flush(); flushErr != nil {
				debug.PrintStack()
				panic(flushErr)
			} else {
				rollbackBatch = rollback
			}

			hldb.ClearWriteHeight()

			cacheInvalidateChan <- feed.Block.Height
		}
	}
}

// Pass this in as an option to use a dbStoreAdapter instead of an IAVLStore for simulation speed.
func fauxMerkleModeOpt(app *baseapp.BaseApp) {
	app.SetFauxMerkleMode()
}

func getGenesisDoc(genesisPath string) *tendermint.GenesisDoc {
	jsonBlob, _ := os.ReadFile(genesisPath)
	shasum := sha1.New()
	shasum.Write(jsonBlob)
	sum := hex.EncodeToString(shasum.Sum(nil))

	log.Printf("[sync] genesis shasum=%s", sum)

	if genesis, genesisErr := tendermint.GenesisDocFromFile(genesisPath); genesisErr != nil {
		panic(genesisErr)
	} else {
		return genesis
	}
}

func forever() {
	<-(chan int)(nil)
}
