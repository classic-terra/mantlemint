package heleveldb

import (
	"fmt"

	dbm "github.com/cometbft/cometbft-db"
)

// DefaultBulkFlushBytes is the buffered size at which a BulkWriter flushes to disk.
const DefaultBulkFlushBytes = 64 * 1024 * 1024

// BulkWriter writes large numbers of keys at a single height with bounded memory.
//
// Unlike LevelBatch it keeps no rollback batch and performs no reads, so it is
// only suitable for populating a database that has no prior state for the keys
// being written. A BulkWriter is not safe for concurrent use; give each
// goroutine its own writer.
type BulkWriter struct {
	driver     *Driver
	height     int64
	flushBytes int

	batch    dbm.Batch
	buffered int
	count    int64
}

func (d *Driver) NewBulkWriter(height int64, flushBytes int) (*BulkWriter, error) {
	if height <= 0 {
		return nil, fmt.Errorf("bulk writer height must be positive, got %d", height)
	}
	if flushBytes <= 0 {
		flushBytes = DefaultBulkFlushBytes
	}

	return &BulkWriter{
		driver:     d,
		height:     height,
		flushBytes: flushBytes,
		batch:      d.session.NewBatch(),
	}, nil
}

// Set buffers key=value at the writer's height, flushing once the buffer is full.
func (w *BulkWriter) Set(key, value []byte) error {
	if w.batch == nil {
		return fmt.Errorf("bulk writer is closed")
	}
	if err := setEntries(w.batch, w.driver.mode, w.height, key, value); err != nil {
		return err
	}

	w.count++
	// current value + height-suffixed value, key three times, plus prefixes/suffix/flag
	w.buffered += 2*len(value) + 3*len(key) + 12
	if w.buffered >= w.flushBytes {
		return w.Flush()
	}
	return nil
}

// Flush writes all buffered entries to disk.
func (w *BulkWriter) Flush() error {
	if w.batch == nil {
		return fmt.Errorf("bulk writer is closed")
	}
	if w.buffered == 0 {
		return nil
	}
	if err := w.batch.Write(); err != nil {
		return err
	}
	// a written batch cannot be reused
	w.batch = w.driver.session.NewBatch()
	w.buffered = 0
	return nil
}

// Close flushes remaining entries durably and releases the writer.
func (w *BulkWriter) Close() error {
	if w.batch == nil {
		return nil
	}
	err := w.batch.WriteSync()
	closeErr := w.batch.Close()
	w.batch = nil
	w.buffered = 0
	if err != nil {
		return err
	}
	return closeErr
}

// Count returns the number of logical keys written so far.
func (w *BulkWriter) Count() int64 {
	return w.count
}
