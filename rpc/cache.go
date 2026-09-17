package rpc

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"

	lru "github.com/hashicorp/golang-lru"
)

type ResponseCache struct {
	status int
	body   []byte
}

type CacheBackend struct {
	lru             *lru.Cache
	evictionCount   uint64
	cacheServeCount uint64
	serveCount      uint64
	cacheType       string
	mtx             *sync.RWMutex

	// requests being processed, by URI; identical requests wait for their result
	inFlight map[string]*inFlightRequest
}

type inFlightRequest struct {
	done     chan struct{}
	response *ResponseCache // set before done is closed; nil if the handler panicked
}

func NewCacheBackend(cacheSize int, cacheType string) *CacheBackend {
	// lru.New
	cache, err := lru.New(cacheSize)
	if err != nil {
		panic(err)
	}

	return &CacheBackend{
		lru:             cache,
		evictionCount:   0,
		cacheServeCount: 0,
		serveCount:      0,
		cacheType:       cacheType,
		mtx:             new(sync.RWMutex),
		inFlight:        make(map[string]*inFlightRequest),
	}
}

func (cb *CacheBackend) Set(cacheKey string, status int, body []byte) *ResponseCache {
	response := &ResponseCache{
		status: status,
		body:   body,
	}
	if evicted := cb.lru.Add(cacheKey, response); evicted {
		cb.evictionCount++
	}

	return response
}

func (cb *CacheBackend) Get(cacheKey string) *ResponseCache {
	cached, ok := cb.lru.Get(cacheKey)
	if !ok {
		return nil
	}

	data, _ := cached.(*ResponseCache)
	return data
}

func (cb *CacheBackend) Metric() {
	fmt.Printf("[rpc/%s] cache length %d, eviction count %d, serveCount %d, cacheServeCount %d\n",
		cb.cacheType,
		cb.lru.Len(),
		cb.evictionCount,
		cb.serveCount,
		cb.cacheServeCount,
	)
}

func (cb *CacheBackend) Purge() {
	cb.mtx.Lock()
	cb.lru.Purge()
	cb.evictionCount = 0
	cb.cacheServeCount = 0
	cb.serveCount = 0
	cb.mtx.Unlock()
}

func (cb *CacheBackend) HandleCachedHTTP(writer http.ResponseWriter, request *http.Request, handler http.Handler) {
	cb.mtx.Lock()
	cb.serveCount++
	cb.mtx.Unlock()

	uri := request.URL.String()

	// see if this request is already made, and in transit
	// set response type as json
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Connection", "close")

	cached := cb.Get(request.URL.String())
	// if cached, return as is
	if cached != nil {
		writer.WriteHeader(cached.status)
		writer.Write(cached.body)

		cb.mtx.Lock()
		cb.cacheServeCount++
		cb.mtx.Unlock()
		return
	}

	cb.mtx.Lock()
	pending, isInTransit := cb.inFlight[uri]
	if isInTransit {
		// same query is processing but not cached yet; wait for its result
		cb.mtx.Unlock()
		<-pending.done
		if pending.response != nil {
			writer.WriteHeader(pending.response.status)
			writer.Write(pending.response.body)
		} else {
			writer.WriteHeader(503)
			writer.Write([]byte("Service Unavailable"))
		}
		return
	}

	// first request for this URI: run the actual querier. Waiters are released
	// when the handler returns, even if it panics.
	pending = &inFlightRequest{done: make(chan struct{})}
	cb.inFlight[uri] = pending
	cb.mtx.Unlock()
	defer func() {
		cb.mtx.Lock()
		delete(cb.inFlight, uri)
		cb.mtx.Unlock()
		close(pending.done)
	}()

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	pending.response = cb.Set(uri, recorder.Code, recorder.Body.Bytes())
	writer.WriteHeader(recorder.Code)
	writer.Write(recorder.Body.Bytes())
}
