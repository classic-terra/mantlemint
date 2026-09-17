package rpc

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCacheBackend(t *testing.T) {
	cb := NewCacheBackend(1, "test")

	cb.Set("key", 200, []byte("hello world"))
	cached := cb.Get("key")
	assert.Equal(t, 200, cached.status)
	assert.Equal(t, []byte("hello world"), cached.body)

	cb.Set("key2", 501, []byte("error"))
	cached2 := cb.Get("key2")
	assert.Equal(t, 501, cached2.status)
	assert.Equal(t, []byte("error"), cached2.body)

	testReq := httptest.NewRequest(
		"get",
		"/test/request?param=1",
		nil,
	)
	testRes := httptest.NewRecorder()
	callCount := 0

	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		callCount++
		writer.WriteHeader(123)
		writer.Write([]byte("asdf"))
	})

	// call 3 times
	cb.HandleCachedHTTP(testRes, testReq, handler)
	cb.HandleCachedHTTP(testRes, testReq, handler)
	cb.HandleCachedHTTP(testRes, testReq, handler)
	cb.HandleCachedHTTP(testRes, testReq, handler)
	cb.HandleCachedHTTP(testRes, testReq, handler)
	cb.HandleCachedHTTP(testRes, testReq, handler)

	fmt.Println(callCount)
	assert.Equal(t, 1, callCount)

	cb.Purge()

	callCount = 0
	cb.HandleCachedHTTP(testRes, testReq, handler)
	cb.HandleCachedHTTP(testRes, testReq, handler)
	cb.HandleCachedHTTP(testRes, testReq, handler)
	cb.HandleCachedHTTP(testRes, testReq, handler)
	cb.HandleCachedHTTP(testRes, testReq, handler)
	cb.HandleCachedHTTP(testRes, testReq, handler)

	fmt.Println(callCount)
	assert.Equal(t, callCount, 1)
}

func TestCacheBackendSharesInFlightRequests(t *testing.T) {
	cb := NewCacheBackend(16, "test")
	release := make(chan struct{})
	var calls atomic.Int32
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		<-release
		writer.WriteHeader(200)
		writer.Write([]byte("shared"))
	})

	leader := httptest.NewRecorder()
	leaderDone := make(chan struct{})
	go func() {
		defer close(leaderDone)
		cb.HandleCachedHTTP(leader, httptest.NewRequest("GET", "/q", nil), handler)
	}()
	assert.Eventually(t, func() bool { return calls.Load() == 1 }, time.Second, time.Millisecond)

	// identical requests arriving while the first is processed wait for its result
	var wg sync.WaitGroup
	waiters := make([]*httptest.ResponseRecorder, 5)
	for i := range waiters {
		waiters[i] = httptest.NewRecorder()
		wg.Add(1)
		go func(w *httptest.ResponseRecorder) {
			defer wg.Done()
			cb.HandleCachedHTTP(w, httptest.NewRequest("GET", "/q", nil), handler)
		}(waiters[i])
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	<-leaderDone

	assert.Equal(t, int32(1), calls.Load())
	for _, w := range append(waiters, leader) {
		assert.Equal(t, 200, w.Code)
		assert.Equal(t, "shared", w.Body.String())
	}

	// once done, a purged URI is processed again instead of waiting on the old request
	cb.Purge()
	cb.HandleCachedHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/q", nil), handler)
	assert.Equal(t, int32(2), calls.Load())
}

func TestCacheBackendReleasesWaitersWhenHandlerPanics(t *testing.T) {
	cb := NewCacheBackend(16, "test")
	started := make(chan struct{})
	release := make(chan struct{})
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		close(started)
		<-release
		panic("query failed")
	})

	leaderPanicked := make(chan any, 1)
	go func() {
		defer func() { leaderPanicked <- recover() }()
		cb.HandleCachedHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/q", nil), handler)
	}()
	<-started

	waiter := httptest.NewRecorder()
	waiterDone := make(chan struct{})
	go func() {
		defer close(waiterDone)
		cb.HandleCachedHTTP(waiter, httptest.NewRequest("GET", "/q", nil), handler)
	}()
	time.Sleep(20 * time.Millisecond)
	close(release)

	assert.Equal(t, "query failed", <-leaderPanicked)
	select {
	case <-waiterDone:
	case <-time.After(time.Second):
		t.Fatal("waiter still blocked after the handler panicked")
	}
	assert.Equal(t, 503, waiter.Code)
}
