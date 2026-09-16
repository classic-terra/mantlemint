package heleveldb

import (
	"errors"
	"fmt"

	"github.com/terra-money/mantlemint/lib"
)

// Import metadata lives under its own prefix, outside the data (0), iterator
// index (1) and height-suffixed (2) key spaces, so it never shows up in reads
// or iteration of application state.
var (
	cImportMetaPrefix = []byte{3}

	importFloorKey = append(append([]byte{}, cImportMetaPrefix...), []byte("floor")...)
	importStateKey = append(append([]byte{}, cImportMetaPrefix...), []byte("state")...)
)

// ErrBelowImportFloor is returned for reads at an explicit height lower than
// the height the database was imported at, where no state exists.
var ErrBelowImportFloor = errors.New("requested height is below the import floor")

type ImportState byte

const (
	// ImportStateNone means the database was not created by an import.
	ImportStateNone ImportState = iota
	ImportStateInProgress
	ImportStateComplete
)

func (s ImportState) String() string {
	switch s {
	case ImportStateNone:
		return "none"
	case ImportStateInProgress:
		return "in-progress"
	case ImportStateComplete:
		return "complete"
	default:
		return fmt.Sprintf("unknown(%d)", byte(s))
	}
}

// ImportFloor returns the lowest height with state, or 0 when there is no floor.
func (d *Driver) ImportFloor() int64 {
	return d.floor
}

// SetImportFloor persists the import floor and enforces it from now on.
func (d *Driver) SetImportFloor(height int64) error {
	if height <= 0 {
		return fmt.Errorf("import floor must be positive, got %d", height)
	}
	if err := d.session.SetSync(importFloorKey, lib.UintToBigEndian(uint64(height))); err != nil {
		return err
	}
	d.floor = height
	return nil
}

func (d *Driver) ImportState() (ImportState, error) {
	v, err := d.session.Get(importStateKey)
	if err != nil {
		return ImportStateNone, err
	}
	if len(v) == 0 {
		return ImportStateNone, nil
	}
	return ImportState(v[0]), nil
}

func (d *Driver) SetImportState(state ImportState) error {
	return d.session.SetSync(importStateKey, []byte{byte(state)})
}

func (d *Driver) loadImportFloor() error {
	v, err := d.session.Get(importFloorKey)
	if err != nil {
		return err
	}
	if len(v) == 0 {
		d.floor = 0
		return nil
	}
	if len(v) != 8 {
		return fmt.Errorf("corrupt import floor record: %d bytes", len(v))
	}
	d.floor = int64(lib.BigEndianToUint(v))
	return nil
}

// checkFloor rejects explicit-height reads below the import floor.
// maxHeight 0 means latest and is always allowed.
func (d *Driver) checkFloor(maxHeight int64) error {
	if maxHeight != 0 && maxHeight < d.floor {
		return fmt.Errorf("%w: height %d, floor %d", ErrBelowImportFloor, maxHeight, d.floor)
	}
	return nil
}
