package turnstile

import (
	"bytes"
	_ "embed"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"

	"github.com/klinoklaz/greenhouse/config"
	"github.com/klinoklaz/greenhouse/util"
)

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
		w.WriteHeader(http.StatusUnauthorized)
		_, err = w.Write(*html.Load())
		return true, err
	}

	// validate token
	addr, _, _ := strings.Cut(util.GetRealIP(r), ":")
	ip, err := netip.ParseAddr(addr)
	if err != nil || ip.IsPrivate() || ip.IsLoopback() {
		addr = ""
	}
	err = verify(tk.Value, addr)
	// sub-resource request
	// (existing cookie, has Referer header)
	if err == nil || (err == alreadyUsed &&
		r.Header.Get("Referer") != "" &&
		r.Header.Get("Sec-Fetch-Dest") != "" &&
		r.Header.Get("Sec-Fetch-Dest") != "document") {
		return false, nil
	}

	w.WriteHeader(http.StatusUnauthorized)
	w.Write(*html.Load())
	return true, err
}
