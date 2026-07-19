package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	bucketChannels = []byte("channels")
	bucketPrograms = []byte("programs")
	bucketState    = []byte("state")
	bucketLogs     = []byte("logs")
)

func (g *Gateway) openCache() error {
	if err := os.MkdirAll(g.cfg.DataDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(g.cfg.DataDir, "iptv-go.bbolt")
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 10 * time.Second})
	if err != nil {
		return err
	}
	g.db = db
	return db.Update(func(tx *bolt.Tx) error {
		for _, name := range [][]byte{bucketChannels, bucketPrograms, bucketState, bucketLogs} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		return nil
	})
}

func (g *Gateway) loadChannels(ctx context.Context) ([]Channel, error) {
	out := []Channel{}
	err := g.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketChannels).ForEach(func(_, v []byte) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			var c Channel
			if err := json.Unmarshal(v, &c); err != nil {
				return err
			}
			out = append(out, c)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sortChannels(out)
	return out, nil
}

func (g *Gateway) saveSnapshot(channels []Channel, programs []Program) error {
	now := time.Now().Unix()
	return g.db.Update(func(tx *bolt.Tx) error {
		cb := tx.Bucket(bucketChannels)
		pb := tx.Bucket(bucketPrograms)
		if err := recreateBucket(tx, bucketChannels); err != nil {
			return err
		}
		if err := recreateBucket(tx, bucketPrograms); err != nil {
			return err
		}
		cb = tx.Bucket(bucketChannels)
		pb = tx.Bucket(bucketPrograms)
		for _, c := range channels {
			c.FetchedAt = now
			b, err := json.Marshal(c)
			if err != nil {
				return err
			}
			if err := cb.Put([]byte(c.ID), b); err != nil {
				return err
			}
		}
		for i, p := range programs {
			p.ID = int64(i + 1)
			b, err := json.Marshal(p)
			if err != nil {
				return err
			}
			if err := pb.Put(programKey(p), b); err != nil {
				return err
			}
		}
		return nil
	})
}

func recreateBucket(tx *bolt.Tx, name []byte) error {
	if err := tx.DeleteBucket(name); err != nil && err != bolt.ErrBucketNotFound {
		return err
	}
	_, err := tx.CreateBucket(name)
	return err
}

func programKey(p Program) []byte {
	return []byte(fmt.Sprintf("%s\x00%020d\x00%020d\x00%s", p.ChannelID, p.Start.Unix(), p.End.Unix(), p.PrevueCode))
}

func channelProgramPrefix(channelID string) []byte {
	return []byte(channelID + "\x00")
}

func (g *Gateway) findProgram(channelID string, start time.Time, margin int) (*Program, error) {
	ts := start.Unix()
	var best *Program
	var bestDelta int64
	err := g.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucketPrograms).Cursor()
		prefix := channelProgramPrefix(channelID)
		for k, v := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v = c.Next() {
			var p Program
			if err := json.Unmarshal(v, &p); err != nil {
				return err
			}
			st := p.Start.Unix()
			en := p.End.Unix()
			if st <= ts+int64(margin) && en > ts-int64(margin) {
				delta := abs64(st - ts)
				if best == nil || delta < bestDelta {
					cp := p
					best = &cp
					bestDelta = delta
				}
			}
		}
		return nil
	})
	return best, err
}

func (g *Gateway) allPrograms() ([]Program, error) {
	out := []Program{}
	err := g.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketPrograms).ForEach(func(_, v []byte) error {
			var p Program
			if err := json.Unmarshal(v, &p); err != nil {
				return err
			}
			out = append(out, p)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ChannelID == out[j].ChannelID {
			return out[i].Start.Before(out[j].Start)
		}
		return out[i].ChannelID < out[j].ChannelID
	})
	return out, nil
}

func (g *Gateway) updatePlayURL(p Program) error {
	return g.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketPrograms)
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var existing Program
			if err := json.Unmarshal(v, &existing); err != nil {
				return err
			}
			if existing.ID != p.ID {
				continue
			}
			existing.PlayURL = p.PlayURL
			existing.PlayURLError = p.PlayURLError
			raw, err := json.Marshal(existing)
			if err != nil {
				return err
			}
			return b.Put(k, raw)
		}
		return nil
	})
}

func (g *Gateway) stateSet(key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return g.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketState).Put([]byte(key), raw)
	})
}

func (g *Gateway) stateGet(key string) (string, error) {
	var raw []byte
	err := g.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketState).Get([]byte(key))
		if v != nil {
			raw = append([]byte(nil), v...)
		}
		return nil
	})
	if err != nil || len(raw) == 0 {
		return "", err
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, nil
	}
	return string(raw), nil
}

func (g *Gateway) stateDelete(key string) error {
	return g.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketState).Delete([]byte(key))
	})
}

func (g *Gateway) counts() (int, int, error) {
	var channels, programs int
	err := g.db.View(func(tx *bolt.Tx) error {
		channels = tx.Bucket(bucketChannels).Stats().KeyN
		programs = tx.Bucket(bucketPrograms).Stats().KeyN
		return nil
	})
	return channels, programs, err
}

func (g *Gateway) addCatchupLog(info CatchupInfo) {
	info.Time = nowLocal().Format(time.RFC3339)
	raw, err := json.Marshal(info)
	if err != nil {
		return
	}
	_ = g.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketLogs)
		id, err := b.NextSequence()
		if err != nil {
			return err
		}
		if err := b.Put(u64Key(id), raw); err != nil {
			return err
		}
		maxLogs := max(1, g.cfg.CatchupLogSize)
		for b.Stats().KeyN > maxLogs {
			c := b.Cursor()
			k, _ := c.First()
			if k == nil {
				break
			}
			if err := c.Delete(); err != nil {
				return err
			}
		}
		return nil
	})
}

func (g *Gateway) loadCatchupLogs() []json.RawMessage {
	out := []json.RawMessage{}
	_ = g.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucketLogs).Cursor()
		for k, v := c.Last(); k != nil && len(out) < max(1, g.cfg.CatchupLogSize); k, v = c.Prev() {
			out = append(out, json.RawMessage(append([]byte(nil), v...)))
		}
		return nil
	})
	return out
}

func u64Key(v uint64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	return b[:]
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
