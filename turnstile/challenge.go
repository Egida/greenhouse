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

func encodeSessionCookie(ttl int16, id uuid.UUID) string {
	var val [2 + 4 + 16 + 32]byte // ttl + timestamp + uuid + digest
	binary.Encode(val[:2], binary.BigEndian, ttl)
	binary.Encode(val[2:6], binary.BigEndian,
		uint32(time.Now().Unix()))
	// id = uuid.New()
	copy(val[6:22], id[:])
	mac := hmac.New(sha256.New, sessoinKey)
	mac.Write(val[:22])
	mac.Sum(val[:22])
	return base64.StdEncoding.EncodeToString(val[:])
}

func decodeSessionCookie(val string) (
	ttl int16, id uuid.UUID, err error) {
	buf, err := base64.StdEncoding.DecodeString(val)
	if err != nil {
		return
	}
	if len(buf) != 2+4+16+32 {
		err = errors.New("invalid session cookie")
		return
	}
	var ts uint32
	binary.Decode(buf[2:6], binary.BigEndian, &ts)
	if time.Since(time.Unix(int64(ts), 0)) > time.Hour {
		err = errors.New("session cookie expired")
		return
	}
	mac := hmac.New(sha256.New, sessoinKey)
	mac.Write(buf[:22])
	if !hmac.Equal(mac.Sum(nil), buf[22:]) {
		err = errors.New("invalid session signature")
		return
	}
	copy(id[:], buf[6:22])
	binary.Decode(buf[:2], binary.BigEndian, &ttl)
	return
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

	immunity, err := r.Cookie("TurnstileImmunity")
	if err == nil {
		ttl, id, err := decodeSessionCookie(immunity.Value)
		if err != nil || ttl <= 0 {
			return true, errors.Join(err, showChallenge(w))
		}
		// not a sub-resource request
		if r.Header.Get("Referer") == "" ||
			r.Header.Get("Sec-Fetch-Dest") == "" ||
			r.Header.Get("Sec-Fetch-Dest") == "document" {
			immunity.Path = "/"
			immunity.Value = encodeSessionCookie(ttl-1, id)
			w.Header().Add("Set-Cookie", immunity.String())
		}
		return false, nil
	}

	token, err := r.Cookie("TurnstileToken")
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
	err = verify(token.Value, addr)
	if err != nil {
		return true, errors.Join(err, showChallenge(w))
	}
	// clear token, issue session cookie
	token.MaxAge = -1
	session := &http.Cookie{
		Path: "/",
		Name: "TurnstileImmunity",
		Value: encodeSessionCookie(
			config.Turnstile().SessionTTL, uuid.New()),
	}
	w.Header().Add("Set-Cookie", token.String())
	w.Header().Add("Set-Cookie", session.String())
	return false, nil
}
