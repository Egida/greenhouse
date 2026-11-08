package turnstile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/klinoklaz/greenhouse/config"
)

const apiURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

var apiClient = &http.Client{
	Timeout: 30 * time.Second,
}

type params struct {
	Secret   string `json:"secret"`
	Response string `json:"response"`
	RemoteIP string `json:"remoteip,omitempty"`
}

type result struct {
	Success     bool      `json:"success"`
	ErrorCodes  []string  `json:"error-codes"`
	ChallengeTs time.Time `json:"challenge_ts"`
}

var alreadyUsed = errors.New("token already used")

// https://developers.cloudflare.com/turnstile/get-started/server-side-validation/
func verify(token, ip string) error {
	var p = params{
		Secret:   config.Turnstile().SecretKey,
		Response: token,
		RemoteIP: ip,
	}
	data, err := json.Marshal(&p)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := apiClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	var r result
	d := json.NewDecoder(res.Body)
	if err = d.Decode(&r); err != nil {
		return err
	}
	if r.Success {
		return nil
	}
	if slices.Contains(r.ErrorCodes, "timeout-or-duplicate") {
		return alreadyUsed
	}
	if len(r.ErrorCodes) > 0 {
		return fmt.Errorf("verification failed, reason [%s]",
			strings.Join(r.ErrorCodes, ", "))
	}
	return fmt.Errorf("unknown failure, api status %d", res.StatusCode)
}
