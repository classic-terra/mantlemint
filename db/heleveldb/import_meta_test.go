package heleveldb

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func writeAt(t *testing.T, d *Driver, height int64, key, value string) {
	t.Helper()
	w, err := d.NewBulkWriter(height, 0)
	assert.Nil(t, err)
	assert.Nil(t, w.Set([]byte(key), []byte(value)))
	assert.Nil(t, w.Close())
}

func TestImportFloorRejectsBelowFloorReads(t *testing.T) {
	d := newTestDriver(t)
	writeAt(t, d, 100, "s/k:bank/a", "1")
	assert.Nil(t, d.SetImportFloor(100))
	assert.Equal(t, int64(100), d.ImportFloor())

	// at and above the floor
	for _, h := range []int64{100, 150} {
		v, err := d.Get(h, []byte("s/k:bank/a"))
		assert.Nil(t, err)
		assert.Equal(t, []byte("1"), v)
	}

	// below the floor, every read path errors
	_, err := d.Get(99, []byte("s/k:bank/a"))
	assert.True(t, errors.Is(err, ErrBelowImportFloor), "get: %v", err)

	_, err = d.Has(99, []byte("s/k:bank/a"))
	assert.True(t, errors.Is(err, ErrBelowImportFloor), "has: %v", err)

	it, err := d.Iterator(99, nil, nil)
	assert.Nil(t, it)
	assert.True(t, errors.Is(err, ErrBelowImportFloor), "iterator: %v", err)

	rit, err := d.ReverseIterator(1, nil, nil)
	assert.Nil(t, rit)
	assert.True(t, errors.Is(err, ErrBelowImportFloor), "reverse iterator: %v", err)
}

func TestImportFloorAllowsLatestReads(t *testing.T) {
	d := newTestDriver(t)
	writeAt(t, d, 100, "s/k:bank/a", "1")
	assert.Nil(t, d.SetImportFloor(100))

	v, err := d.Get(0, []byte("s/k:bank/a"))
	assert.Nil(t, err)
	assert.Equal(t, []byte("1"), v)

	it, err := d.Iterator(0, nil, nil)
	assert.Nil(t, err)
	assert.True(t, it.Valid())
	assert.Nil(t, it.Close())
}

func TestNoFloorKeepsExistingBehavior(t *testing.T) {
	d := newTestDriver(t)
	writeAt(t, d, 100, "s/k:bank/a", "1")
	assert.Equal(t, int64(0), d.ImportFloor())

	// below the only write: not found, no error, as before
	v, err := d.Get(1, []byte("s/k:bank/a"))
	assert.Nil(t, err)
	assert.Nil(t, v)

	it, err := d.Iterator(1, nil, nil)
	assert.Nil(t, err)
	assert.False(t, it.Valid())
	assert.Nil(t, it.Close())
}

func TestImportFloorPersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	cfg := &DriverConfig{Name: "test", Dir: dir, Mode: DriverModeKeySuffixDesc}

	d, err := NewLevelDBDriver(cfg)
	assert.Nil(t, err)
	assert.Nil(t, d.SetImportFloor(250))
	assert.Nil(t, d.Close())

	d, err = NewLevelDBDriver(cfg)
	assert.Nil(t, err)
	defer d.Close()
	assert.Equal(t, int64(250), d.ImportFloor())
	_, err = d.Get(249, []byte("any"))
	assert.True(t, errors.Is(err, ErrBelowImportFloor))
}

func TestImportFloorRejectsNonPositive(t *testing.T) {
	d := newTestDriver(t)
	assert.NotNil(t, d.SetImportFloor(0))
	assert.Equal(t, int64(0), d.ImportFloor())
}

func TestImportStateRoundTrip(t *testing.T) {
	d := newTestDriver(t)

	s, err := d.ImportState()
	assert.Nil(t, err)
	assert.Equal(t, ImportStateNone, s)

	assert.Nil(t, d.SetImportState(ImportStateInProgress))
	s, err = d.ImportState()
	assert.Nil(t, err)
	assert.Equal(t, ImportStateInProgress, s)

	assert.Nil(t, d.SetImportState(ImportStateComplete))
	s, err = d.ImportState()
	assert.Nil(t, err)
	assert.Equal(t, ImportStateComplete, s)
}

func TestImportMetadataInvisibleToIteration(t *testing.T) {
	d := newTestDriver(t)
	writeAt(t, d, 10, "a", "1")
	assert.Nil(t, d.SetImportFloor(10))
	assert.Nil(t, d.SetImportState(ImportStateComplete))

	for _, h := range []int64{0, 10} {
		it, err := d.Iterator(h, nil, nil)
		assert.Nil(t, err)
		var keys []string
		for ; it.Valid(); it.Next() {
			keys = append(keys, string(it.Key()))
		}
		assert.Nil(t, it.Close())
		assert.Equal(t, []string{"a"}, keys, "height %d", h)
	}
}
