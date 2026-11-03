package config

import (
	"net/http"
	"net/url"
	"time"
)

type cache struct {
	Capacity int64 // Max cache size, default 1 GB
}

var Cache = &cache{
	Capacity: 1 << 30,
}

type server struct {
	ListenAddr string `json:"listen_addr"`

	IdleTimeout  time.Duration // Corresponds to http.Server.IdleTimeout
	ReadTimeout  time.Duration // Corresponds to http.Server.ReadTimeout
	WriteTimeout time.Duration // Corresponds to http.Server.WriteTimeout

	// Cancel request if upstream doesn't respond within this amount of time
	FetchTimeout time.Duration
}

var Server = &server{
	ListenAddr:   ":3456",
	IdleTimeout:  30 * time.Second,
	ReadTimeout:  30 * time.Second,
	WriteTimeout: 30 * time.Second,
	FetchTimeout: 30 * time.Second,
}

type log struct {
	Location string `json:"location"` // Log file location
	UseJSON  bool   `json:"use_json"` // Format logs as JSON
}

var Log = &log{
	Location: "stderr",
}

type rules struct {
	// Map of [request host => upstream target]
	Targets map[string]*url.URL

	// Set this if you don't need to deal with multiple hosts
	Target *url.URL

	// List of HTTP status codes that are cacheable
	StatusFilter []int `json:"status_filter"`

	// Response will change along with these request headers.
	// Adding `Origin` to prevent CORS error is recommended
	VaryHeaders []string `json:"vary_headers"`

	// Dictates whether to bypass caching if the request is auth related,
	// the server detects common auth info such as cookie / auth headers.
	//
	// When set to false, auth headers are stripped from request
	// to prevent user-specific or privileged content being cached.
	// Note that some auth mechanisms like CSRF token validation may still break
	// despite this option being turned on, due to the nature of reverse proxy
	AllowAuth bool `json:"allow_auth"`

	// Whether to detect mobile UA and cache the responses separately
	CheckMobileUA bool `json:"check_mobile_ua"`
}

func (r *rules) GetTarget(host string) *url.URL {
	if r.SingleTarget() {
		return r.Target
	}
	return r.Targets[host]
}

func (r *rules) SingleTarget() bool {
	return r.Target != nil
}

var Rules = &rules{
	StatusFilter: []int{http.StatusOK},
}
