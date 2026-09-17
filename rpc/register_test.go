package rpc

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCacheMiddlewareCachesOnlyGET(t *testing.T) {
	calls := 0
	var gotHeight string
	handler := cacheMiddleware(NewCacheBackend(16, "latest"), NewCacheBackend(16, "archival"))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			gotHeight = r.Header.Get("x-cosmos-block-height")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(r.Method))
		}))
	serve := func(method, url, body string) {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(method, url, strings.NewReader(body)))
	}

	serve(http.MethodGet, "/cosmos/bank/v1beta1/supply", "")
	serve(http.MethodGet, "/cosmos/bank/v1beta1/supply", "")
	assert.Equal(t, 1, calls, "repeated GET is served from cache")

	serve(http.MethodGet, "/cosmos/bank/v1beta1/supply?height=5", "")
	assert.Equal(t, "5", gotHeight)
	serve(http.MethodGet, "/cosmos/bank/v1beta1/supply?height=5", "")
	assert.Equal(t, 2, calls, "repeated GET at a height is served from the archival cache")

	// same URL, different bodies: each must reach the handler
	serve(http.MethodPost, "/cosmos/tx/v1beta1/simulate", `{"tx_bytes":"a"}`)
	serve(http.MethodPost, "/cosmos/tx/v1beta1/simulate", `{"tx_bytes":"b"}`)
	assert.Equal(t, 4, calls)

	serve(http.MethodGet, "/health", "")
	serve(http.MethodGet, "/health", "")
	assert.Equal(t, 6, calls)
}
