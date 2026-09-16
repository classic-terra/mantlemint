package importer

import (
	"errors"
	"fmt"
	"sort"

	storetypes "cosmossdk.io/store/types"
	dbm "github.com/cosmos/cosmos-db"
	gogotypes "github.com/cosmos/gogoproto/types"
	"github.com/cosmos/iavl"
	idb "github.com/cosmos/iavl/db"
	"github.com/syndtr/goleveldb/leveldb/filter"
	"github.com/syndtr/goleveldb/leveldb/opt"
)

// Key layout of a cosmos-sdk rootmulti store, shared by terrad and mantlemint.
const (
	appDBName        = "application"
	latestVersionKey = "s/latest"
	commitInfoKeyFmt = "s/%d"
	storeKeyPrefix   = "s/k:%s/"

	// small node cache: traversal visits each node once, so caching buys little
	iavlNodeCacheSize = 10_000
)

// AppStateSource reads module state out of a stopped terrad node's application.db.
type AppStateSource struct {
	db         dbm.DB
	height     int64
	commitInfo *storetypes.CommitInfo
	infoBytes  []byte
}

// OpenAppState opens <dataDir>/application.db read-only and resolves the import
// height. A height of 0 selects the latest committed version.
func OpenAppState(dataDir string, height int64) (*AppStateSource, error) {
	db, err := dbm.NewGoLevelDBWithOpts(appDBName, dataDir, &opt.Options{
		ReadOnly:               true,
		Filter:                 filter.NewBloomFilter(10),
		OpenFilesCacheCapacity: 1024,
	})
	if err != nil {
		return nil, fmt.Errorf("open %s.db in %s read-only (is terrad still running?): %w", appDBName, dataDir, err)
	}

	src, err := newAppStateSource(db, height)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return src, nil
}

func newAppStateSource(db dbm.DB, height int64) (*AppStateSource, error) {
	latest, err := readLatestVersion(db)
	if err != nil {
		return nil, err
	}
	if latest == 0 {
		return nil, fmt.Errorf("no committed version found (%q missing): not an application database", latestVersionKey)
	}
	if height == 0 {
		height = latest
	}
	if height > latest {
		return nil, fmt.Errorf("height %d is above the latest committed version %d", height, latest)
	}

	infoBytes, err := db.Get([]byte(fmt.Sprintf(commitInfoKeyFmt, height)))
	if err != nil {
		return nil, err
	}
	if infoBytes == nil {
		return nil, fmt.Errorf("no commit info at height %d: the height was pruned or never committed", height)
	}
	info := &storetypes.CommitInfo{}
	if err := info.Unmarshal(infoBytes); err != nil {
		return nil, fmt.Errorf("decode commit info at height %d: %w", height, err)
	}

	return &AppStateSource{
		db:         db,
		height:     height,
		commitInfo: info,
		infoBytes:  infoBytes,
	}, nil
}

func readLatestVersion(db dbm.DB) (int64, error) {
	bz, err := db.Get([]byte(latestVersionKey))
	if err != nil || bz == nil {
		return 0, err
	}
	var latest int64
	if err := gogotypes.StdInt64Unmarshal(&latest, bz); err != nil {
		return 0, fmt.Errorf("decode %s: %w", latestVersionKey, err)
	}
	return latest, nil
}

func (s *AppStateSource) Height() int64 {
	return s.height
}

// CommitInfoBytes returns the raw commit info record at the import height.
func (s *AppStateSource) CommitInfoBytes() []byte {
	return s.infoBytes
}

// StoreNames lists the stores committed at the import height, sorted.
func (s *AppStateSource) StoreNames() []string {
	names := make([]string, 0, len(s.commitInfo.StoreInfos))
	for _, info := range s.commitInfo.StoreInfos {
		names = append(names, info.Name)
	}
	sort.Strings(names)
	return names
}

// IterateStore streams every leaf of a store at the import height in key order.
// It returns the number of leaves visited and fails if that number differs
// from the leaf count recorded in the tree root.
func (s *AppStateSource) IterateStore(name string, fn func(key, value []byte) error) (count int64, err error) {
	// iavl panics on some malformed nodes; report those like any other read failure
	defer recoverInto(&err, fmt.Sprintf("store %q: read tree at height %d", name, s.height))

	prefixed := dbm.NewPrefixDB(s.db, []byte(fmt.Sprintf(storeKeyPrefix, name)))
	// skipFastStorageUpgrade must be true: otherwise loading may rewrite the
	// whole fast-node index, which cannot work on a read-only database
	tree := iavl.NewMutableTree(idb.NewWrapper(prefixed), iavlNodeCacheSize, true, iavl.NewNopLogger())

	if !tree.VersionExists(s.height) {
		return 0, fmt.Errorf("store %q: version %d is not available (pruned or never written)", name, s.height)
	}
	itree, err := tree.GetImmutable(s.height)
	if err != nil {
		if errors.Is(err, iavl.ErrVersionDoesNotExist) {
			return 0, fmt.Errorf("store %q: version %d is not available (pruned or never written): %w", name, s.height, err)
		}
		return 0, fmt.Errorf("store %q: load version %d: %w", name, s.height, err)
	}

	expected := itree.Size()
	if expected > 0 {
		it, err := itree.Iterator(nil, nil, true)
		if err != nil {
			return 0, fmt.Errorf("store %q: open iterator at %d: %w", name, s.height, err)
		}
		for ; it.Valid(); it.Next() {
			if err := fn(it.Key(), it.Value()); err != nil {
				_ = it.Close()
				return count, err
			}
			count++
		}
		// iavl's iterator stops silently on a node read failure; surface it
		iterErr := it.Error()
		_ = it.Close()
		if iterErr != nil {
			return count, fmt.Errorf("store %q: tree read failed at height %d after %d leaves (corrupt or partially pruned): %w",
				name, s.height, count, iterErr)
		}
	}

	if count != expected {
		return count, fmt.Errorf("store %q: visited %d leaves at height %d but the tree records %d", name, count, s.height, expected)
	}
	return count, nil
}

func (s *AppStateSource) Close() error {
	return s.db.Close()
}
