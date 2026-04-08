package usage

import (
	"encoding/json"
	"errors"
	"fmt"

	badger "github.com/dgraph-io/badger/v4"
)

// UsageStore persists usage records locally using BadgerDB
type UsageStore struct {
	db *badger.DB
}

// NewUsageStore creates a new usage store
func NewUsageStore(dir string) (*UsageStore, error) {
	opts := badger.DefaultOptions(dir)
	opts.Logger = nil // Disable logging for tests

	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("opening badger db: %w", err)
	}

	return &UsageStore{db: db}, nil
}

// Close closes the database
func (s *UsageStore) Close() error {
	return s.db.Close()
}

// SaveRecord persists a usage record
func (s *UsageStore) SaveRecord(record *UsageRecord) error {
	key := []byte(fmt.Sprintf("record/%s/%s", record.RequestID, record.MetricName))

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshalling record: %w", err)
	}

	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Set(key, data)
	})
}

// GetRecord retrieves a usage record by request ID and metric name.
func (s *UsageStore) GetRecord(requestID string, metricName string) (*UsageRecord, error) {
	var record UsageRecord

	key := []byte(fmt.Sprintf("record/%s/%s", requestID, metricName))

	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &record)
		})
	})

	if err != nil {
		return nil, err
	}

	return &record, nil
}

// GetPendingRecords retrieves unaggregated records for a specific consumer/provider/service/metric
func (s *UsageStore) GetPendingRecords(consumer, provider, service, metric string) ([]*UsageRecord, error) {
	var records []*UsageRecord

	prefix := []byte("record/")

	err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var record UsageRecord
				if err := json.Unmarshal(val, &record); err != nil {
					return err
				}

				// Filter by criteria
				if record.ConsumerPeer == consumer &&
					record.ProviderPeer == provider &&
					record.Service == service &&
					record.MetricName == metric {
					records = append(records, &record)
				}

				return nil
			})
			if err != nil {
				return err
			}
		}

		return nil
	})

	return records, err
}

// MarkAggregated removes records that have been aggregated.
// Each element in records must be a "requestID/metricName" compound key
// matching the format used by SaveRecord.
func (s *UsageStore) MarkAggregated(records []*UsageRecord) error {
	return s.db.Update(func(txn *badger.Txn) error {
		for _, r := range records {
			key := []byte(fmt.Sprintf("record/%s/%s", r.RequestID, r.MetricName))
			if err := txn.Delete(key); err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
				return err
			}
		}
		return nil
	})
}

// SaveAggregate stores an aggregated usage record
func (s *UsageStore) SaveAggregate(agg *AggregatedUsage) error {
	key := []byte(fmt.Sprintf("aggregate/%s/%s/%s/%d",
		agg.PeerID, agg.Service, agg.MetricName, agg.WindowStart))

	data, err := json.Marshal(agg)
	if err != nil {
		return fmt.Errorf("marshalling aggregate: %w", err)
	}

	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Set(key, data)
	})
}
