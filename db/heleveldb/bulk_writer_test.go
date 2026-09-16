package heleveldb

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func newTestDriver(t *testing.T) *Driver {
	t.Helper()
	d, err := NewLevelDBDriver(&DriverConfig{
		Name: "test",
		Dir:  t.TempDir(),
		Mode: DriverModeKeySuffixDesc,
	})
	assert.Nil(t, err)
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestBulkWriterReadsBack(t *testing.T) {
	d := newTestDriver(t)

	w, err := d.NewBulkWriter(100, 0)
	assert.Nil(t, err)
	assert.Nil(t, w.Set([]byte("s/k:bank/a"), []byte("1")))
	assert.Nil(t, w.Set([]byte("s/k:bank/b"), []byte("2")))
	assert.Nil(t, w.Set([]byte("s/k:bank/c"), []byte("3")))
	assert.Nil(t, w.Close())
	assert.Equal(t, int64(3), w.Count())

	// latest height
	v, err := d.Get(0, []byte("s/k:bank/b"))
	assert.Nil(t, err)
	assert.Equal(t, []byte("2"), v)

	// explicit height, at and above the written height
	for _, h := range []int64{100, 101, 5000} {
		v, err = d.Get(h, []byte("s/k:bank/b"))
		assert.Nil(t, err)
		assert.Equal(t, []byte("2"), v, "height %d", h)
	}

	has, err := d.Has(100, []byte("s/k:bank/c"))
	assert.Nil(t, err)
	assert.True(t, has)
}

func TestBulkWriterPopulatesIteratorIndex(t *testing.T) {
	d := newTestDriver(t)

	w, err := d.NewBulkWriter(100, 0)
	assert.Nil(t, err)
	// written out of order on purpose
	for _, k := range []string{"c", "a", "b"} {
		assert.Nil(t, w.Set([]byte("s/k:bank/"+k), []byte("v"+k)))
	}
	assert.Nil(t, w.Close())

	// explicit-height iteration walks the iterator key index
	it, err := d.Iterator(100, []byte("s/k:bank/"), []byte("s/k:bank0"))
	assert.Nil(t, err)
	var keys, values []string
	for ; it.Valid(); it.Next() {
		keys = append(keys, string(it.Key()))
		values = append(values, string(it.Value()))
	}
	assert.Nil(t, it.Close())
	assert.Equal(t, []string{"s/k:bank/a", "s/k:bank/b", "s/k:bank/c"}, keys)
	assert.Equal(t, []string{"va", "vb", "vc"}, values)

	rit, err := d.ReverseIterator(100, []byte("s/k:bank/"), []byte("s/k:bank0"))
	assert.Nil(t, err)
	keys = nil
	for ; rit.Valid(); rit.Next() {
		keys = append(keys, string(rit.Key()))
	}
	assert.Nil(t, rit.Close())
	assert.Equal(t, []string{"s/k:bank/c", "s/k:bank/b", "s/k:bank/a"}, keys)
}

func TestBulkWriterFlushesAcrossThreshold(t *testing.T) {
	d := newTestDriver(t)

	// tiny threshold forces many intermediate flushes
	w, err := d.NewBulkWriter(7, 64)
	assert.Nil(t, err)
	const n = 1000
	for i := 0; i < n; i++ {
		assert.Nil(t, w.Set([]byte(fmt.Sprintf("key/%05d", i)), []byte(fmt.Sprintf("value-%d", i))))
	}
	assert.Nil(t, w.Close())
	assert.Equal(t, int64(n), w.Count())

	for i := 0; i < n; i++ {
		v, err := d.Get(7, []byte(fmt.Sprintf("key/%05d", i)))
		assert.Nil(t, err)
		assert.Equal(t, []byte(fmt.Sprintf("value-%d", i)), v)
	}
}

func TestBulkWriterEmptyValue(t *testing.T) {
	d := newTestDriver(t)

	w, err := d.NewBulkWriter(10, 0)
	assert.Nil(t, err)
	assert.Nil(t, w.Set([]byte("empty"), []byte{}))
	assert.Nil(t, w.Close())

	v, err := d.Get(10, []byte("empty"))
	assert.Nil(t, err)
	assert.NotNil(t, v)
	assert.Empty(t, v)

	has, err := d.Has(10, []byte("empty"))
	assert.Nil(t, err)
	assert.True(t, has)
}

func TestBulkWriterMatchesLevelBatch(t *testing.T) {
	injected := newTestDriver(t)
	imported := newTestDriver(t)

	entries := [][2]string{
		{"s/latest", "\x00\x01"},
		{"s/k:bank/balance", "100uluna"},
		{"s/k:wasm/code", "bytes"},
		{"s/k:wasm/empty", ""},
	}

	batch := NewLevelDBBatch(42, injected)
	w, err := imported.NewBulkWriter(42, 0)
	assert.Nil(t, err)
	for _, e := range entries {
		assert.Nil(t, batch.Set([]byte(e[0]), []byte(e[1])))
		assert.Nil(t, w.Set([]byte(e[0]), []byte(e[1])))
	}
	assert.Nil(t, batch.WriteSync())
	assert.Nil(t, batch.Close())
	assert.Nil(t, w.Close())

	assert.Equal(t, rawEntries(t, injected), rawEntries(t, imported))
}

func TestBulkWriterRejectsInvalidUse(t *testing.T) {
	d := newTestDriver(t)

	_, err := d.NewBulkWriter(0, 0)
	assert.NotNil(t, err)

	w, err := d.NewBulkWriter(1, 0)
	assert.Nil(t, err)
	assert.Nil(t, w.Close())
	assert.NotNil(t, w.Set([]byte("k"), []byte("v")))
	assert.Nil(t, w.Close())
}

func rawEntries(t *testing.T, d *Driver) map[string]string {
	t.Helper()
	it, err := d.session.Iterator(nil, nil)
	assert.Nil(t, err)
	defer it.Close()
	out := map[string]string{}
	for ; it.Valid(); it.Next() {
		out[string(it.Key())] = string(it.Value())
	}
	return out
}
