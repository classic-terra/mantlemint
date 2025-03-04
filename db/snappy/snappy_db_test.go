package snappy

import (
	"io"
	"os"
	"testing"

	dbm "github.com/cometbft/cometbft-db"
	tmjson "github.com/cometbft/cometbft/libs/json"
	tendermint "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/assert"
)

func TestSnappyDB(t *testing.T) {
	snappy := NewSnappyDB(dbm.NewMemDB(), CompatModeEnabled)

	assert.Nil(t, snappy.Set([]byte("test"), []byte("testValue")))

	var v []byte
	var err error

	// nil buffer test
	v, err = snappy.Get([]byte("non-existing"))
	assert.Nil(t, v)
	assert.Nil(t, err)

	v, err = snappy.Get([]byte("test"))
	assert.Nil(t, err)
	assert.Equal(t, []byte("testValue"), v)

	assert.Nil(t, snappy.Delete([]byte("test")))
	v, err = snappy.Get([]byte("test"))
	assert.Nil(t, v)
	assert.Nil(t, err)

	// iterator is not supported
	var it dbm.Iterator
	it, err = snappy.Iterator([]byte("start"), []byte("end"))
	assert.Nil(t, it)
	assert.Equal(t, errIteratorNotSupported, err)

	it, err = snappy.ReverseIterator([]byte("start"), []byte("end"))
	assert.Nil(t, it)
	assert.Equal(t, errIteratorNotSupported, err)

	// batched store is compressed as well
	var batch dbm.Batch
	batch = snappy.NewBatch()

	assert.Nil(t, batch.Set([]byte("key"), []byte("batchedValue")))
	assert.Nil(t, batch.Write())
	assert.Nil(t, batch.Close())

	v, err = snappy.Get([]byte("key"))
	assert.Equal(t, []byte("batchedValue"), v)
	assert.Nil(t, err)

	batch = snappy.NewBatch()
	assert.Nil(t, batch.Delete([]byte("key")))
	assert.Nil(t, batch.Write())
	assert.Nil(t, batch.Close())

	v, err = snappy.Get([]byte("key"))
	assert.Nil(t, v)
	assert.Nil(t, err)
}

func TestSnappyDBCompat(t *testing.T) {
	mdb := dbm.NewMemDB()
	testKey := []byte("testKey")

	nocompat := NewSnappyDB(mdb, CompatModeDisabled)
	indexSampleTx(nocompat, testKey)

	nocompatResult, _ := nocompat.Get(testKey)

	compat := NewSnappyDB(mdb, CompatModeEnabled)
	compatResult, _ := compat.Get(testKey)
	assert.Equal(t, nocompatResult, compatResult)

	nocompatResult2, _ := nocompat.Get(testKey)
	assert.Equal(t, compatResult, nocompatResult2)
}

func indexSampleTx(mdb dbm.DB, key []byte) {
	block := &tendermint.Block{}
	blockFile, _ := os.Open("../../indexer/fixtures/block_4814775.json")
	blockJSON, _ := io.ReadAll(blockFile)
	if err := tmjson.Unmarshal(blockJSON, block); err != nil {
		panic(err)
	}

	_ = mdb.Set(key, blockJSON)
}
