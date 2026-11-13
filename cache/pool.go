package cache

import (
	"math/bits"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/klinoklaz/greenhouse/config"
	"github.com/klinoklaz/greenhouse/util"
	"github.com/zeebo/xxh3"
)

type lookup struct {
	table   map[Hash][]*Entry
	mu      sync.Mutex
	estSize int // estimated total size in bytes
}

// (not concurrent safe, lock l.mu before use)
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
	l.estSize -= e.hSize()
	if len(list) > 0 {
		l.table[e.digest] = list
	} else {
		l.estSize -= len(e.Body)
		delete(l.table, e.digest)
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
	lastEvictionCost time.Duration
}

// pool is the main cache pool
var pool = store{
	data:    make(map[Hash]*Entry),
	toEvict: make(chan util.Void),
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
			if v.admitted() {
				list = append(list, item{k: k, v: v})
			}
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
		pool.lastEvictionCost = time.Since(t)

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
	if e.admitted() {
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
	if e == nil || !e.admitted() {
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

// Commit is called after a cache miss (which results in
// new Entry being created) to resolve any inconsistent state
func Commit(e *Entry, key Hash, admit bool) {
	close(*e.ready.Load())

	if !admit {
		pool.mu.Lock()
		delete(pool.data, key)
		pool.mu.Unlock()
		return
	}

	defer e.admit()
	hSize := e.hSize()

	dupLookup.mu.Lock()
	defer dupLookup.mu.Unlock()
	dupLookup.estSize += hSize
	// if body size is no bigger than pointer size,
	// it's not worth deduplicating by saving a copy of *Entry
	if len(e.Body) <= bits.UintSize/8 {
		return
	}
	e.digest = xxh3.Hash128(e.Body)
	list := dupLookup.table[e.digest]
	// merge duplication
	if len(list) > 0 {
		e.Body = list[0].Body
	} else {
		dupLookup.estSize += len(e.Body)
	}
	dupLookup.table[e.digest] = append(list, e)
	// trigger eviction
	if dupLookup.estSize > config.Cache().Capacity {
		go func() { pool.toEvict <- util.VoidVal }()
	}
}
