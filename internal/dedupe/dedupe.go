// Package dedupe provides a small thread-safe LRU of recently-seen content
// hashes, shared by the folder and clipboard pipelines so a single capture
// that arrives via both paths (e.g. Win+Shift+S) is only uploaded once.
package dedupe

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// LRU is a fixed-capacity set of hashes with least-recently-used eviction.
type LRU struct {
	mu  sync.Mutex
	cap int
	ll  *list.List               // front = most recently used
	set map[string]*list.Element // hash -> element
}

// New returns an LRU holding up to capacity hashes.
func New(capacity int) *LRU {
	if capacity <= 0 {
		capacity = 256
	}
	return &LRU{
		cap: capacity,
		ll:  list.New(),
		set: make(map[string]*list.Element, capacity),
	}
}

// SeenOrAdd records the hash and returns true if it was already present
// (i.e. the content is a duplicate that should be skipped).
func (l *LRU) SeenOrAdd(hash string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if el, ok := l.set[hash]; ok {
		l.ll.MoveToFront(el)
		return true
	}
	el := l.ll.PushFront(hash)
	l.set[hash] = el
	if l.ll.Len() > l.cap {
		back := l.ll.Back()
		if back != nil {
			l.ll.Remove(back)
			delete(l.set, back.Value.(string))
		}
	}
	return false
}

// HashBytes returns the hex SHA-256 of b.
func HashBytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
