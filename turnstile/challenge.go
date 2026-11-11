package turnstile

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/klinoklaz/greenhouse/config"
	"github.com/klinoklaz/greenhouse/util"
)

//go:embed challenge.html
var htmlTpl []byte

var html atomic.Pointer[[]byte]

var sessoinKey []byte

func Init() {
	// pros: zero config
	// cons: all sessions are invalidated after program restart
	if sessoinKey == nil {
		sessoinKey = make([]byte, 32)
		rand.Read(sessoinKey)
	}

	if t := config.Turnstile(); t.ChallengePage != "" {
		content, _ := os.ReadFile(t.ChallengePage)
		html.Store(&content)
	} else {
		content := bytes.Replace(htmlTpl,
			[]byte("__SITE_KEY__"), []byte(t.SiteKey), 1)
		html.Store(&content)
	}
}

func showChallenge(w http.ResponseWriter) error {
	w.WriteHeader(http.StatusUnauthorized)
	_, err := w.Write(*html.Load())
	return err
}

func newSessionCookie() string {
	var val [8 + 16 + 32]byte // timestamp + uuid + signature
	binary.Encode(val[:8], binary.BigEndian, time.Now().Unix())
	id := uuid.New()
	copy(val[8:24], id[:])
	mac := hmac.New(sha256.New, sessoinKey)
	mac.Write(val[:24])
	mac.Sum(val[:24])
	return base64.StdEncoding.EncodeToString(val[:])
}

func checkSessionCookie(val string, ttl time.Duration) bool {
	buf, err := base64.StdEncoding.DecodeString(val)
	if err != nil {
		return false
	}
	if len(buf) != 8+16+32 {
		return false
	}
	var ts int64
	binary.Decode(buf[:8], binary.BigEndian, &ts)
	if time.Since(time.Unix(ts, 0)) > ttl {
		return false // expired
	}
	mac := hmac.New(sha256.New, sessoinKey)
	mac.Write(buf[:24])
	if !hmac.Equal(mac.Sum(nil), buf[24:]) {
		return false // invalid signature
	}
	return true
}

const (
	SessionName = "TurnstileImmuneSession"
	TokenName   = "TurnstileToken"
)

func ChallengeHTTP(w http.ResponseWriter, r *http.Request) (
	abort bool, err error) {
	tConfig := config.Turnstile()
	if !tConfig.Enabled() {
		return false, nil
	}
	defer func() {
		if err != nil {
			err = fmt.Errorf("turnstile challenge: %w", err)
		}
	}()

	immunity, err := r.Cookie(SessionName)
	if err == nil && checkSessionCookie(
		immunity.Value, tConfig.SessionTTL.Val) {
		return false, nil
	}

	token, err := r.Cookie(TokenName)
	// show challenge page
	if err != nil {
		return true, showChallenge(w)
	}

	// validate token
	addr, _, _ := strings.Cut(util.GetRealIP(r), ":")
	ip, err := netip.ParseAddr(addr)
	if err != nil || ip.IsPrivate() || ip.IsLoopback() {
		addr = ""
	}
	if err = verify(token.Value, addr); err != nil {
		return true, errors.Join(err, showChallenge(w))
	}
	// clear token, issue session cookie
	token.MaxAge = -1
	session := &http.Cookie{
		Path:  "/",
		Name:  SessionName,
		Value: newSessionCookie(),
	}
	w.Header().Add("Set-Cookie", token.String())
	w.Header().Add("Set-Cookie", session.String())
	return false, nil
}
