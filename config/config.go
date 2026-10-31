package config

type cache struct {
	// Max cache size, default 1 GB
	Capacity uint64
	// Response will change along with these request headers
	VaryHeaders []string `json:"vary_headers"`
}

var Cache = &cache{}
