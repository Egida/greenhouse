package turnstile

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/klinoklaz/greenhouse/config"
	"github.com/klinoklaz/greenhouse/util"
)

type tokenStatus struct {
	t     time.Time
	valid bool
	ready chan util.Void
}

var tokenTmpStore struct {
	tk map[string]*tokenStatus
	mu sync.Mutex
}

func init() {
	tokenTmpStore.tk = make(map[string]*tokenStatus)
	go func() {
		// periodically purge stored tokens
		for {
			time.Sleep(2 * time.Minute)
			tokenTmpStore.mu.Lock()
			for k, v := range tokenTmpStore.tk {
				if time.Since(v.t) > 2*time.Minute {
					delete(tokenTmpStore.tk, k)
				}
			}
			tokenTmpStore.mu.Unlock()
		}
	}()
}

//go:embed challenge.html
var htmlTpl []byte

var html atomic.Pointer[[]byte]

func Init() {
	b := bytes.Replace(htmlTpl,
		[]byte("__SITE_KEY__"),
		[]byte(config.Turnstile().SiteKey), 1)
	html.Store(&b)
}

func ChallengeHTTP(w http.ResponseWriter, r *http.Request) (
	abort bool, err error) {
	if !config.Turnstile().Enabled() {
		return false, nil
	}
	defer func() {
		if err != nil {
			err = fmt.Errorf("turnstile challenge: %w", err)
		}
	}()

	tk, err := r.Cookie("TurnstileToken")
	// show challenge page
	if err != nil {
		w.WriteHeader(http.StatusTooManyRequests)
		_, err = w.Write(*html.Load())
		return true, err
	}

	tokenTmpStore.mu.Lock()
	// sub-resource request
	// (existing cookie, has Referer header)
	status, ok := tokenTmpStore.tk[tk.Value]
	if ok && r.Header.Get("Referer") != "" {
		tokenTmpStore.mu.Unlock()
		select {
		case <-status.ready:
			if !status.valid {
				w.WriteHeader(http.StatusForbidden)
			}
			return !status.valid, nil
		case <-r.Context().Done():
			w.WriteHeader(http.StatusGatewayTimeout)
			return true, errors.New("request cancelled")
		}
	}

	status = &tokenStatus{
		t:     time.Now(),
		ready: make(chan util.Void),
	}
	defer close(status.ready)
	tokenTmpStore.tk[tk.Value] = status
	tokenTmpStore.mu.Unlock()

	// validate token
	addr, _, _ := strings.Cut(util.GetRealIP(r), ":")
	ip, err := netip.ParseAddr(addr)
	if err != nil || ip.IsPrivate() || ip.IsLoopback() {
		addr = ""
	}
	err = verify(tk.Value, addr)
	status.valid = err == nil
	if !status.valid {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("403 Forbidden: verification failed"))
	}
	return !status.valid, err
}
