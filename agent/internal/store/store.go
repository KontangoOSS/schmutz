package store

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	bucketJA4       = []byte("ja4")        // JA4 fingerprint → seen count + first/last seen
	bucketSNI       = []byte("sni")        // SNI → hit count + first/last seen
	bucketRateLimit = []byte("rate_limit") // source IP → window counters
	bucketSessions  = []byte("sessions")   // service name → cached session metadata
	bucketStats     = []byte("stats")      // global counters
)

// Store provides persistent local state backed by BoltDB.
// Each edge node has its own store — no cross-node replication needed.
// The Ziti controller Raft cluster handles distributed state.
// BoltDB is for local observability and rate limiting only.
type Store struct {
	db *bolt.DB
}

// JA4Record tracks a TLS fingerprint.
type JA4Record struct {
	Fingerprint string    `json:"fingerprint"`
	Count       uint64    `json:"count"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
	LastSNI     string    `json:"last_sni"`
	LastSrcIP   string    `json:"last_src_ip"`
	Action      string    `json:"action"` // last action taken (route/drop)
}

// SNIRecord tracks a requested hostname.
type SNIRecord struct {
	Hostname  string    `json:"hostname"`
	Count     uint64    `json:"count"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	LastJA4   string    `json:"last_ja4"`
	LastSrcIP string    `json:"last_src_ip"`
}

// Open creates or opens a BoltDB at the given path.
func Open(path string) (*Store, error) {
	db, err := bolt.Open(path, 0600, &bolt.Options{
		Timeout:      5 * time.Second,
		NoGrowSync:   false,
		FreelistType: bolt.FreelistMapType,
	})
	if err != nil {
		return nil, fmt.Errorf("open bolt: %w", err)
	}

	// Create all buckets on startup
	err = db.Update(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{bucketJA4, bucketSNI, bucketRateLimit, bucketSessions, bucketStats} {
			if _, err := tx.CreateBucketIfNotExists(b); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create buckets: %w", err)
	}

	return &Store{db: db}, nil
}

// Close cleanly shuts down the BoltDB.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying BoltDB for use by other subsystems (e.g., health pool).
func (s *Store) DB() *bolt.DB {
	return s.db
}

// RecordJA4 updates the JA4 fingerprint counter.
func (s *Store) RecordJA4(fingerprint, sni, srcIP, action string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketJA4)
		key := []byte(fingerprint)
		now := time.Now().UTC()

		var rec JA4Record
		if data := b.Get(key); data != nil {
			json.Unmarshal(data, &rec)
		} else {
			rec.Fingerprint = fingerprint
			rec.FirstSeen = now
		}

		rec.Count++
		rec.LastSeen = now
		rec.LastSNI = sni
		rec.LastSrcIP = srcIP
		rec.Action = action

		data, err := json.Marshal(&rec)
		if err != nil {
			return err
		}
		return b.Put(key, data)
	})
}

// RecordSNI updates the SNI hostname counter.
func (s *Store) RecordSNI(hostname, ja4, srcIP string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSNI)
		key := []byte(hostname)
		now := time.Now().UTC()

		var rec SNIRecord
		if data := b.Get(key); data != nil {
			json.Unmarshal(data, &rec)
		} else {
			rec.Hostname = hostname
			rec.FirstSeen = now
		}

		rec.Count++
		rec.LastSeen = now
		rec.LastJA4 = ja4
		rec.LastSrcIP = srcIP

		data, err := json.Marshal(&rec)
		if err != nil {
			return err
		}
		return b.Put(key, data)
	})
}

// CheckRateLimit returns true if the source IP is within rate limits.
// windowSec is the rate window in seconds, maxCount is the max allowed.
func (s *Store) CheckRateLimit(srcIP string, windowSec, maxCount int) (bool, error) {
	var allowed bool
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketRateLimit)
		now := time.Now().Unix()
		windowStart := now - int64(windowSec)

		// Key format: srcIP/window_epoch
		// Each window gets its own counter
		windowEpoch := now / int64(windowSec)
		key := []byte(fmt.Sprintf("%s/%d", srcIP, windowEpoch))

		var count uint64
		if data := b.Get(key); data != nil && len(data) == 8 {
			count = binary.BigEndian.Uint64(data)
		}

		if count >= uint64(maxCount) {
			allowed = false
			return nil
		}

		count++
		data := make([]byte, 8)
		binary.BigEndian.PutUint64(data, count)
		allowed = true

		// Clean up old windows (best effort)
		c := b.Cursor()
		prefix := []byte(srcIP + "/")
		for k, _ := c.Seek(prefix); k != nil; k, _ = c.Next() {
			if len(k) <= len(prefix) {
				break
			}
			// Only delete keys that match this srcIP prefix and are old
			if string(k[:len(prefix)]) != string(prefix) {
				break
			}
			// Parse window epoch from key
			if string(k) != string(key) {
				var oldEpoch int64
				fmt.Sscanf(string(k[len(prefix):]), "%d", &oldEpoch)
				if oldEpoch*int64(windowSec) < windowStart {
					c.Delete()
				}
			}
		}

		return b.Put(key, data)
	})
	return allowed, err
}

// IncrStat increments a named global counter.
func (s *Store) IncrStat(name string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketStats)
		key := []byte(name)
		var count uint64
		if data := b.Get(key); data != nil && len(data) == 8 {
			count = binary.BigEndian.Uint64(data)
		}
		count++
		data := make([]byte, 8)
		binary.BigEndian.PutUint64(data, count)
		return b.Put(key, data)
	})
}

// GetStat reads a named global counter.
func (s *Store) GetStat(name string) (uint64, error) {
	var count uint64
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketStats)
		if data := b.Get([]byte(name)); data != nil && len(data) == 8 {
			count = binary.BigEndian.Uint64(data)
		}
		return nil
	})
	return count, err
}

// ListJA4 returns all tracked JA4 fingerprints.
func (s *Store) ListJA4() ([]JA4Record, error) {
	var records []JA4Record
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketJA4)
		return b.ForEach(func(k, v []byte) error {
			var rec JA4Record
			if err := json.Unmarshal(v, &rec); err != nil {
				return nil // Skip corrupt entries
			}
			records = append(records, rec)
			return nil
		})
	})
	return records, err
}

// ListSNI returns all tracked SNI hostnames.
func (s *Store) ListSNI() ([]SNIRecord, error) {
	var records []SNIRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSNI)
		return b.ForEach(func(k, v []byte) error {
			var rec SNIRecord
			if err := json.Unmarshal(v, &rec); err != nil {
				return nil
			}
			records = append(records, rec)
			return nil
		})
	})
	return records, err
}
