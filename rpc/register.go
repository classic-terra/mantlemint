package rpc

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	sdklog "cosmossdk.io/log"
	terra "github.com/classic-terra/core/v4/app"
	"github.com/classic-terra/core/v4/app/params"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server/api"
	"github.com/cosmos/cosmos-sdk/server/config"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/gorilla/mux"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
)

func StartRPC(
	app *terra.TerraApp,
	rpcclient rpcclient.Client,
	chainId string,
	codec params.EncodingConfig,
	invalidateTrigger chan int64,
	registerCustomRoutes func(router *mux.Router),
	getIsSynced func() bool,
) error {
	vp := viper.GetViper()
	cfg, _ := config.GetConfig(vp)

	// create terra client; register all codecs
	clientCtx := client.
		Context{}.
		WithClient(rpcclient).
		WithCodec(codec.Marshaler).
		WithInterfaceRegistry(codec.InterfaceRegistry).
		WithTxConfig(codec.TxConfig).
		WithAccountRetriever(authtypes.AccountRetriever{}).
		WithLegacyAmino(codec.Amino).
		WithHomeDir(terra.DefaultNodeHome).
		WithChainID(chainId)

	// create backends for response cache
	// - cache: used for latest states without `height` parameter
	// - archivalCache: used for historical states with `height` parameter; never flushed
	cache := NewCacheBackend(16384, "latest")
	archivalCache := NewCacheBackend(16384, "archival")

	// register cache invalidator
	go func() {
		for {
			height := <-invalidateTrigger
			fmt.Printf("[cache-middleware] purging cache at height %d\n", height)

			cache.Metric()
			archivalCache.Metric()

			// only purge latest cache
			cache.Purge()
		}
	}()

	// start new api server
	apiSrv := api.New(clientCtx, sdklog.NewNopLogger(), grpc.NewServer())

	// register custom routes to default api server
	registerCustomRoutes(apiSrv.Router)

	// custom healthcheck endpoint
	apiSrv.Router.Handle("/health", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		isSynced := getIsSynced()
		if isSynced {
			writer.WriteHeader(http.StatusOK)
			writer.Write([]byte("OK"))
		} else {
			writer.WriteHeader(http.StatusServiceUnavailable)
			writer.Write([]byte("NOK"))
		}
	})).Methods("GET")

	// register all default GET routers...
	app.RegisterAPIRoutes(apiSrv, cfg.API)
	app.RegisterTendermintService(clientCtx)
	errCh := make(chan error)
	serverCtx := context.Background()

	// caching middleware
	apiSrv.Router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path == "/health" {
				next.ServeHTTP(writer, request)
				return
			}

			heightQuery := request.URL.Query().Get("height")
			height, err := strconv.ParseInt(heightQuery, 10, 64)

			// don't use archival cache if height is 0 or error
			if err == nil && height > 0 {
				// GRPC query parses height from header
				request.Header.Add("x-cosmos-block-height", heightQuery)
				archivalCache.HandleCachedHTTP(writer, request, next)
			} else {
				cache.HandleCachedHTTP(writer, request, next)
			}
		})
	})

	// start api server in goroutine
	go func() {
		if err := apiSrv.Start(serverCtx, cfg); err != nil {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-time.After(100 * time.Millisecond): // assume server started successfully
	}

	return nil
}
