package ghx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The GitHub CLI's own OAuth application.
//
// A device flow needs a client id and GitHub issues them only to registered
// applications; this is the one `gh auth login --web` uses. It is public by
// construction — a device-flow client has no secret, which is the entire
// reason the flow exists — and using it is what makes the token indis-
// tinguishable from one gh minted itself. That matters more than it looks:
// gh is what stores the token, gh's credential helper is what git asks for it,
// and the authorisation the operator sees on github.com says "GitHub CLI",
// which is the truth about what will be using it.
const (
	deviceClientID = "178c6fc778ccc68e1d6a"
	deviceCodeURL  = "https://github.com/login/device/code"
	deviceTokenURL = "https://github.com/login/oauth/access_token"

	// The scopes gh itself requests. `repo` covers push and pull requests,
	// `read:org` is what lists an organisation's repositories, and `workflow`
	// is the one whose absence is discovered late and confusingly: without it
	// the remote refuses any push whose commits touch .github/workflows.
	deviceScopes = "repo read:org gist workflow"
)

// deviceFlow is one sign-in in progress. It lives on the server, and the
// device code — which is a bearer credential until it is redeemed — never
// reaches the browser: the page holds an opaque id and polls with that, and
// the access token that comes back goes straight into gh's own store.
type deviceFlow struct {
	deviceCode string
	dir        string
	host       string
	interval   time.Duration
	next       time.Time
	expires    time.Time
}

// DeviceStart is what the page needs to show: the code to type and where to
// type it.
type DeviceStart struct {
	ID              string `json:"id"`
	UserCode        string `json:"userCode"`
	VerificationURI string `json:"verificationUri"`
	ExpiresIn       int    `json:"expiresIn"`
	Interval        int    `json:"interval"`
}

// DeviceState is one answer to "has it happened yet".
type DeviceState struct {
	Status  string   `json:"status"` // pending, complete, denied, expired
	Account *Account `json:"account,omitempty"`
	Message string   `json:"message,omitempty"`
}

// StartDevice asks GitHub for a code the operator can type into their own
// browser. It is the only outbound request this package makes on its own
// account, and it is made because somebody pressed sign in.
func (s *Service) StartDevice(ctx context.Context, dir string) (*DeviceStart, error) {
	if !s.Available() {
		return nil, ErrNotInstalled
	}
	var body struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
		Error           string `json:"error"`
		Description     string `json:"error_description"`
	}
	if err := s.form(ctx, deviceCodeURL, url.Values{
		"client_id": {deviceClientID},
		"scope":     {deviceScopes},
	}, &body); err != nil {
		return nil, err
	}
	if body.Error != "" {
		return nil, fmt.Errorf("github refused the sign-in request: %s", describeOAuthError(body.Error, body.Description))
	}
	if body.DeviceCode == "" || body.UserCode == "" {
		return nil, errors.New("github returned no device code")
	}
	if body.Interval <= 0 {
		body.Interval = 5
	}
	if body.ExpiresIn <= 0 {
		body.ExpiresIn = 900
	}
	id := randomID()
	s.mu.Lock()
	s.sweepLocked()
	s.flows[id] = &deviceFlow{
		deviceCode: body.DeviceCode,
		dir:        dir,
		host:       DefaultHost,
		interval:   time.Duration(body.Interval) * time.Second,
		next:       time.Now(),
		expires:    time.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
	}
	s.mu.Unlock()
	return &DeviceStart{
		ID:              id,
		UserCode:        body.UserCode,
		VerificationURI: body.VerificationURI,
		ExpiresIn:       body.ExpiresIn,
		Interval:        body.Interval,
	}, nil
}

// PollDevice asks GitHub whether the code has been entered yet, and finishes
// the sign-in when it has.
//
// The interval GitHub asks for is enforced here rather than trusted to the
// page: a browser that polls too fast is answered "pending" from the flow's
// own clock without a request leaving the server, because GitHub's remedy for
// polling too fast is to slow the whole flow down.
func (s *Service) PollDevice(ctx context.Context, id string) (*DeviceState, error) {
	s.mu.Lock()
	flow, ok := s.flows[id]
	if !ok {
		s.mu.Unlock()
		return &DeviceState{Status: "expired", Message: "this sign-in has expired — start it again"}, nil
	}
	if time.Now().After(flow.expires) {
		delete(s.flows, id)
		s.mu.Unlock()
		return &DeviceState{Status: "expired", Message: "the code expired before it was entered"}, nil
	}
	if time.Now().Before(flow.next) {
		s.mu.Unlock()
		return &DeviceState{Status: "pending"}, nil
	}
	flow.next = time.Now().Add(flow.interval)
	deviceCode, dir, host := flow.deviceCode, flow.dir, flow.host
	s.mu.Unlock()

	var body struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := s.form(ctx, deviceTokenURL, url.Values{
		"client_id":   {deviceClientID},
		"device_code": {deviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}, &body); err != nil {
		return nil, err
	}

	switch body.Error {
	case "":
	case "authorization_pending":
		return &DeviceState{Status: "pending"}, nil
	case "slow_down":
		s.mu.Lock()
		if f, ok := s.flows[id]; ok {
			f.interval += 5 * time.Second
			f.next = time.Now().Add(f.interval)
		}
		s.mu.Unlock()
		return &DeviceState{Status: "pending"}, nil
	case "expired_token":
		s.drop(id)
		return &DeviceState{Status: "expired", Message: "the code expired before it was entered"}, nil
	case "access_denied":
		s.drop(id)
		return &DeviceState{Status: "denied", Message: "the sign-in was cancelled on github.com"}, nil
	default:
		s.drop(id)
		return &DeviceState{Status: "denied", Message: describeOAuthError(body.Error, body.Description)}, nil
	}
	if body.AccessToken == "" {
		s.drop(id)
		return &DeviceState{Status: "denied", Message: "github returned no token"}, nil
	}

	s.drop(id)
	acc, err := s.LoginWithToken(ctx, dir, host, body.AccessToken)
	if err != nil {
		return nil, err
	}
	return &DeviceState{Status: "complete", Account: acc}, nil
}

func (s *Service) drop(id string) {
	s.mu.Lock()
	delete(s.flows, id)
	s.mu.Unlock()
}

// sweepLocked discards flows nobody came back for. Without it a dashboard left
// open for a month accumulates one entry per abandoned sign-in.
func (s *Service) sweepLocked() {
	now := time.Now()
	for id, f := range s.flows {
		if now.After(f.expires) {
			delete(s.flows, id)
		}
	}
}

func (s *Service) form(ctx context.Context, endpoint string, values url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Without this GitHub answers form-encoded, which is the older default and
	// silently parses as an empty struct.
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "just-dashboard")
	res, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach github.com: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 500 {
		return fmt.Errorf("github.com answered %s", res.Status)
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// describeOAuthError turns GitHub's machine codes into the sentence the
// operator needs, keeping the original where there is nothing to add.
func describeOAuthError(code, description string) string {
	switch code {
	case "unsupported_grant_type", "incorrect_client_credentials", "incorrect_device_code":
		return "github rejected the sign-in request (" + code + ")"
	case "device_flow_disabled":
		return "device sign-in is disabled for this application — sign in with a token instead"
	}
	if description != "" {
		return description
	}
	return code
}

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
