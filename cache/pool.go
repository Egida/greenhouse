package cache

import (
	"runtime/metrics"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/klinoklaz/greenhouse/config"
	"github.com/klinoklaz/greenhouse/util"
)

var memUseInit uint64

func getMemUse() uint64 {
	var sample = []metrics.Sample{
		{Name: "/memory/classes/heap/objects:bytes"},
	}
	metrics.Read(sample)
	return sample[0].Value.Uint64()
}

func Prepare() {
	memUseInit = getMemUse()
	go evict()
}

type lookup struct {
	table map[Hash][]*Entry
	mu    sync.Mutex
}

func (l *lookup) insert(e *Entry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	list := l.table[e.digest]
	// merge duplication
	if len(list) > 0 {
		e.Body = list[0].Body
	}
	list = append(list, e)
	l.table[e.digest] = list
}

// (no concurrent safe, lock l.mu before use)
func (l *lookup) delete(e *Entry) (isLast bool) {
	list := l.table[e.digest]
	if list == nil {
		return false
	}
	for i, v := range list {
		if v == e {
			list = slices.Delete(list, i, i+1)
			break
		}
	}
	isLast = len(list) == 0
	if isLast {
		delete(l.table, e.digest)
	} else {
		l.table[e.digest] = list
	}
	return isLast
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
	lastEvict time.Time
}

func (s *store) checkMemUse() {
	if getMemUse()-memUseInit > config.Cache.Capacity {
		go func() { s.toEvict <- util.VoidVal }()
	}
}

// pool is the main cache pool
var pool = store{
	data:    make(map[Hash]*Entry),
	toEvict: make(chan util.Void),
}

func evict() {
	for range pool.toEvict {
		// shrink the pool size to a portion of capacity
		// to prevent constant triggering of eviction
		goal := config.Cache.Capacity * 4 / 5
		memUse := getMemUse() - memUseInit

		pool.mu.Lock()
		dupLookup.mu.Lock()

		list := make([]*Entry, 0, len(pool.data))
		for _, e := range pool.data {
			list = append(list, e)
		}
		slices.SortFunc(list, (*Entry).cmp)
		for _, e := range list {
			if memUse <= goal {
				break
			}
			if dupLookup.delete(e) {
				memUse -= uint64(len(e.Body)) + 1024
			} else {
				memUse -= 1024 // an arbitrary estimation
			}
			delete(pool.data, e.key)
		}

		pool.mu.Unlock()
		dupLookup.mu.Unlock()
	}
}

const (
	Ok = iota
	Init
	Wait
)

func Get(key Hash) (e *Entry, state int8) {
	pool.mu.RLock()
	e = pool.data[key]
	// direct hit
	if e != nil {
		pool.mu.RUnlock()
		e.hit.incr()
		pool.hit.Add(1)
		return e, Ok
	}
	pool.mu.RUnlock()
	// try again
	pool.mu.Lock()
	e = pool.data[key]
	// inited by someone else
	if e != nil {
		pool.mu.Unlock()
		e.hit.incr()
		pool.hit.Add(1)
		return e, Wait
	}
	// first request
	e = newEntry(key)
	pool.data[key] = e
	pool.mu.Unlock()
	pool.miss.Add(1)
	return e, Init
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
