package config

import (
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"go.yaml.in/yaml/v3"
)

type duration struct {
	Val time.Duration
}

func (d *duration) Valid() bool {
	return d.Val > 0
}

// (implements json.Unmarshaler)
func (d *duration) UnmarshalJSON(data []byte) error {
	if len(data) < 3 {
		return nil
	}
	data = data[1 : len(data)-1] // unquote
	pd, err := time.ParseDuration(string(data))
	if err == nil {
		d.Val = pd
	}
	return err
}

// (implements yaml.Unmarshaler)
func (d *duration) UnmarshalYAML(value *yaml.Node) error {
	pd, err := time.ParseDuration(value.Value)
	if err == nil {
		d.Val = pd
	}
	return err
}

type urlurl struct {
	Val *url.URL
}

func (u *urlurl) Valid() bool {
	return u.Val.Host != "" && u.Val.Scheme != ""
}

// (implements json.Unmarshaler)
func (u *urlurl) UnmarshalJSON(data []byte) error {
	if len(data) < 3 {
		return nil
	}
	data = data[1 : len(data)-1]
	pu, err := url.Parse(string(data))
	if err == nil {
		u.Val = pu
	}
	return err
}

// (implements yaml.Unmarshaler)
func (u *urlurl) UnmarshalYAML(value *yaml.Node) error {
	if value.Value == "" {
		return nil
	}
	pu, err := url.Parse(value.Value)
	if err == nil {
		u.Val = pu
	}
	return err
}

type cache struct {
	Capacity    int64  `json:"-" yaml:"-"` // Max cache size, default 1 GB
	CapacityStr string `json:"capacity" yaml:"capacity"`
}

type server struct {
	ListenAddr string `json:"listen_addr" yaml:"listen_addr"`

	// Corresponds to http.Server.IdleTimeout
	IdleTimeout duration `json:"idle_timeout" yaml:"idle_timeout"`

	// Corresponds to http.Server.ReadTimeout
	ReadTimeout duration `json:"read_timeout" yaml:"read_timeout"`

	// Corresponds to http.Server.WriteTimeout
	WriteTimeout duration `json:"write_timeout" yaml:"write_timeout"`

	// Cancel request if upstream doesn't respond within this amount of time
	FetchTimeout duration `json:"fetch_timeout" yaml:"fetch_timeout"`
}

// turnstile is the cloudflare turnstile config
type turnstile struct {
	SiteKey   string `json:"site_key" yaml:"site_key"`
	SecretKey string `json:"secret_key" yaml:"secret_key"`

	// Path to an html file of custom challenge page,
	// you need to include the site key into the file by yourself
	ChallengePage string `json:"challenge_page" yaml:"challenge_page"`

	// Users are immune to another challenge in this duration
	// after completing one
	SessionTTL duration `json:"session_ttl" yaml:"session_ttl"`

	// TODO: add multi-target support
}

func (t *turnstile) Enabled() bool {
	return (t.SiteKey != "" || t.ChallengePage != "") && t.SecretKey != ""
}

type log struct {
	// Log level, e.g. debug, info, warn, error, panic, fatal
	Level string `json:"level" yaml:"level"`

	UseJSON  bool   `json:"use_json" yaml:"use_json"` // Format logs as JSON
	Location string `json:"location" yaml:"location"` // Log file location
}

type rules struct {
	// Map of [request host => upstream target].
	// This isn't a load balancer, it's only useful
	// if you have multiple different websites
	Targets map[string]urlurl `json:"targets" yaml:"targets"`

	// Set this if you don't need to deal with multiple hosts
	Target urlurl `json:"target" yaml:"target"`

	// List of HTTP status codes that are cacheable
	StatusFilter []int `json:"status_filter" yaml:"status_filter"`

	// Response will change along with these request headers.
	// Adding `Origin` to prevent CORS error is recommended
	VaryHeaders []string `json:"vary_headers" yaml:"vary_headers"`

	// Dictates whether to bypass caching if the request is auth related,
	// the server detects common auth info such as cookie / auth headers.
	//
	// When set to false, auth headers are stripped from request
	// to prevent user-specific or privileged content being cached.
	// Note that some auth mechanisms like CSRF token validation may still break
	// despite this option being turned on, due to the nature of reverse proxy
	AllowAuth bool `json:"allow_auth" yaml:"allow_auth"`

	// Whether to detect mobile UA and cache the responses separately
	CheckMobile bool `json:"check_mobile" yaml:"check_mobile"`
}

func (r *rules) GetTarget(host string) *url.URL {
	if r.SingleTarget() {
		return r.Target.Val
	}
	return r.Targets[host].Val
}

func (r *rules) SingleTarget() bool {
	return r.Target.Val != nil
}

type everything struct {
	Cache     cache     `json:"cache" yaml:"cache"`
	Server    server    `json:"server" yaml:"server"`
	Log       log       `json:"log" yaml:"log"`
	Rules     rules     `json:"rules" yaml:"rules"`
	Turnstile turnstile `json:"turnstile" yaml:"turnstile"`
}

var e atomic.Pointer[everything]

var defaultE = everything{
	Cache: cache{
		Capacity: 1 << 30,
	},
	Server: server{
		ListenAddr:   ":3456",
		IdleTimeout:  duration{30 * time.Second},
		ReadTimeout:  duration{30 * time.Second},
		WriteTimeout: duration{30 * time.Second},
		FetchTimeout: duration{30 * time.Second},
	},
	Log: log{
		Level:    "info",
		Location: "stderr",
	},
	Rules: rules{
		StatusFilter: []int{http.StatusOK},
	},
	Turnstile: turnstile{
		SessionTTL: duration{30 * time.Minute},
	},
}

func init() {
	e.Store(&defaultE)
}
