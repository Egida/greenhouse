package config

type cache struct {
	Capacity int64 // Max cache size, default 1 GB
}

var Cache = &cache{
	Capacity: 1 << 30,
}
