package heleveldb

import "github.com/syndtr/goleveldb/leveldb/opt"

type DriverConfig struct {
	Name string
	Dir  string
	Mode int
	// Options tunes the underlying goleveldb; nil uses cometbft-db's defaults.
	Options *opt.Options
}
