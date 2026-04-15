package rootmulti

import (
	cmdb "github.com/cometbft/cometbft-db"
	cosmosdb "github.com/cosmos/cosmos-db"
)

type cosmosDBAdapter struct {
	db cmdb.DB
}

func newCosmosDBAdapter(db cmdb.DB) cosmosdb.DB {
	if db == nil {
		return nil
	}

	return cosmosDBAdapter{db: db}
}

func NewCosmosDBAdapter(db cmdb.DB) cosmosdb.DB {
	return newCosmosDBAdapter(db)
}

func (a cosmosDBAdapter) Get(key []byte) ([]byte, error)              { return a.db.Get(key) }
func (a cosmosDBAdapter) Has(key []byte) (bool, error)                { return a.db.Has(key) }
func (a cosmosDBAdapter) Set(key, value []byte) error                 { return a.db.Set(key, value) }
func (a cosmosDBAdapter) SetSync(key, value []byte) error             { return a.db.SetSync(key, value) }
func (a cosmosDBAdapter) Delete(key []byte) error                     { return a.db.Delete(key) }
func (a cosmosDBAdapter) DeleteSync(key []byte) error                 { return a.db.DeleteSync(key) }
func (a cosmosDBAdapter) Iterator(start, end []byte) (cosmosdb.Iterator, error) {
	return a.db.Iterator(start, end)
}
func (a cosmosDBAdapter) ReverseIterator(start, end []byte) (cosmosdb.Iterator, error) {
	return a.db.ReverseIterator(start, end)
}
func (a cosmosDBAdapter) Close() error               { return a.db.Close() }
func (a cosmosDBAdapter) Print() error               { return a.db.Print() }
func (a cosmosDBAdapter) Stats() map[string]string   { return a.db.Stats() }
func (a cosmosDBAdapter) NewBatch() cosmosdb.Batch   { return cosmosBatchAdapter{batch: a.db.NewBatch()} }
func (a cosmosDBAdapter) NewBatchWithSize(_ int) cosmosdb.Batch {
	return cosmosBatchAdapter{batch: a.db.NewBatch()}
}

type cosmosBatchAdapter struct {
	batch cmdb.Batch
}

func (a cosmosBatchAdapter) Set(key, value []byte) error { return a.batch.Set(key, value) }
func (a cosmosBatchAdapter) Delete(key []byte) error     { return a.batch.Delete(key) }
func (a cosmosBatchAdapter) Write() error                { return a.batch.Write() }
func (a cosmosBatchAdapter) WriteSync() error            { return a.batch.WriteSync() }
func (a cosmosBatchAdapter) Close() error                { return a.batch.Close() }
func (a cosmosBatchAdapter) GetByteSize() (int, error)   { return 0, nil }
