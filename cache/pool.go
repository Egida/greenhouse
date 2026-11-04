package cache

import (
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/klinoklaz/greenhouse/config"
	"github.com/klinoklaz/greenhouse/util"
)

type lookup struct {
	table   map[Hash][]*Entry
	mu      sync.Mutex
	estSize int64 // estimated total size in bytes
}

// (no concurrent safe, lock l.mu before use)
func (l *lookup) delete(e *Entry) {
	list, ok := l.table[e.digest]
	if !ok {
		return
	}
	for i, v := range list {
		if v == e {
			list = slices.Delete(list, i, i+1)
			break
		}
	}
	if len(list) == 0 {
		l.estSize -= 1024 + int64(len(e.Body))
		delete(l.table, e.digest)
	} else {
		l.estSize -= 1024
		l.table[e.digest] = list
	}
}

// dupLookup is the lookup table
// that records unique body hashes for deduplication
var dupLookup = lookup{
	table: make(map[Hash][]*Entry),
}

type store struct {
	data      map[Hash]*Entry
	mu        sync.RWMutex
	hit, miss atomic.Uint64
	toEvict   chan util.Void

	evictionCnt      int
	lastEvict        time.Time
	lsatEvictionCost time.Duration
}

// pool is the main cache pool
var pool = store{
	data:    make(map[Hash]*Entry),
	toEvict: make(chan util.Void),
}

func deduplicate(e *Entry) {
	dupLookup.mu.Lock()
	list := dupLookup.table[e.digest]
	// merge duplication
	if len(list) > 0 {
		dupLookup.estSize += 1024 // http headers etc.
		e.Body = list[0].Body
	} else {
		dupLookup.estSize += 1024 + int64(len(e.Body))
	}
	dupLookup.table[e.digest] = append(list, e)
	dupLookup.mu.Unlock()
	// trigger eviction
	if dupLookup.estSize > config.Cache().Capacity {
		go func() { pool.toEvict <- util.VoidVal }()
	}
}

func evict() {
	type item struct {
		k Hash
		v *Entry
	}
	for range pool.toEvict {
		// shrink the pool size to a portion of capacity
		// to prevent constant triggering of eviction
		goal := config.Cache().Capacity * 4 / 5

		t := time.Now()
		pool.mu.Lock()
		dupLookup.mu.Lock()

		// the easy way: sort + traverse
		list := make([]item, 0, len(pool.data))
		for k, v := range pool.data {
			list = append(list, item{k: k, v: v})
		}
		slices.SortFunc(list, func(a, b item) int {
			return a.v.cmp(b.v)
		})
		for _, i := range list {
			dupLookup.delete(i.v)
			delete(pool.data, i.k)
			if dupLookup.estSize <= goal {
				break
			}
		}

		// statisitics
		pool.evictionCnt++
		pool.lastEvict = t
		pool.lsatEvictionCost = time.Since(t)

		pool.mu.Unlock()
		dupLookup.mu.Unlock()
	}
}

// (not idempotent)
func Init() {
	go evict()
}

const (
	Ok = iota
	Miss
	Wait
)

func afterFound(e *Entry) int8 {
	pool.hit.Add(1)
	e.hit.incr()
	if e.ready == nil {
		return Ok
	} else {
		return Wait
	}
}

func Get(key Hash) (e *Entry, state int8) {
	pool.mu.RLock()
	e = pool.data[key]
	// direct hit
	if e != nil {
		pool.mu.RUnlock()
		return e, afterFound(e)
	}
	pool.mu.RUnlock()
	// try again
	pool.mu.Lock()
	e = pool.data[key]
	// inited by someone else
	if e != nil {
		pool.mu.Unlock()
		return e, afterFound(e)
	}
	// first request
	e = newEntry()
	pool.data[key] = e
	pool.mu.Unlock()
	pool.miss.Add(1)
	return e, Miss
}

func Del(key Hash) {
	pool.mu.RLock()
	e := pool.data[key]
	pool.mu.RUnlock()
	if e == nil {
		return
	}
	// delete lookup entry first
	// to prevent e.Body being reused
	dupLookup.mu.Lock()
	dupLookup.delete(e)
	dupLookup.mu.Unlock()
	pool.mu.Lock()
	delete(pool.data, key)
	pool.mu.Unlock()
}
