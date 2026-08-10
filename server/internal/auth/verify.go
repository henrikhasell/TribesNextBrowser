// Package auth answers one question: does this (guid, uuid) pair belong to a
// player who really controls that TribesNext account?
//
// It does not answer it itself. TribesNext already exposes a verification
// oracle: any third party holding a pair can call json_browser.php and gets 200
// if the session is live and 401 otherwise. tn_challenge_isAuthorized($guid,
// $uuid) takes both, so a token cannot be replayed against a different GUID.
//
// That is the whole identity model, and it is why this server holds no
// passwords, no keys, and needs nothing extracted from IFC22.dll. The client
// keeps logging in to TribesNext with its RSA key exactly as before; we just
// ask upstream whether the resulting token is real.
//
// The trade is a hard dependency: if TribesNext is unreachable, nobody can log
// in here either. The cache below keeps steady-state traffic to one upstream
// call per player per TTL rather than one per request.
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultUpstream is TribesNext's browser endpoint.
const DefaultUpstream = "https://tribesnext.thyth.com/tn/json/json_browser.php"

// Identity is what a successful verification tells us.
type Identity struct {
	GUID string
	Name string // authoritative TribesNext display name
	// Created is the account's TribesNext registration date, in unix seconds,
	// or 0 when upstream did not give a usable one. Zero means "we do not
	// know" and not "the epoch" -- the store falls back to first sighting.
	Created int64
}

type entry struct {
	id      Identity
	expires time.Time
}

type Verifier struct {
	upstream string
	client   *http.Client
	ttl      time.Duration
	log      *slog.Logger

	mu    sync.Mutex
	cache map[string]entry

	// now is swappable for tests.
	now func() time.Time
}

func NewVerifier(upstream string, ttl time.Duration) *Verifier {
	if upstream == "" {
		upstream = DefaultUpstream
	}
	if ttl <= 0 {
		// Matches the client's session refresh interval: re-checking more often
		// than the session itself changes buys nothing.
		ttl = 10 * time.Minute
	}
	return &Verifier{
		upstream: upstream,
		client:   &http.Client{Timeout: 20 * time.Second},
		ttl:      ttl,
		log:      slog.Default(),
		cache:    map[string]entry{},
		now:      time.Now,
	}
}

// SetLogger replaces the logger. Defaulted in NewVerifier so no call site has
// to pass one.
func (v *Verifier) SetLogger(l *slog.Logger) {
	if l != nil {
		v.log = l
	}
}

// ErrUnauthorised means upstream rejected the pair. Anything else is a fault on
// our side or theirs, and must not be reported to the player as "bad session" --
// telling someone their login is invalid when the real problem is that
// TribesNext is down sends them off resetting credentials for no reason.
var ErrUnauthorised = fmt.Errorf("session not recognised by TribesNext")

// Verify checks a pair, using the cache when it can.
func (v *Verifier) Verify(ctx context.Context, guid, uuid string) (Identity, error) {
	if guid == "" || uuid == "" {
		return Identity{}, ErrUnauthorised
	}

	key := guid + "\x00" + uuid
	v.mu.Lock()
	if e, ok := v.cache[key]; ok && v.now().Before(e.expires) {
		v.mu.Unlock()
		return e.id, nil
	}
	v.mu.Unlock()

	id, err := v.ask(ctx, guid, uuid)
	if err != nil {
		return Identity{}, err
	}

	v.mu.Lock()
	v.cache[key] = entry{id: id, expires: v.now().Add(v.ttl)}
	v.mu.Unlock()
	return id, nil
}

// ask performs the upstream check.
//
// userview for the caller's own GUID is used because it doubles as an identity
// lookup: the same response carries the authoritative display name, so
// verifying and learning who they are is one round trip rather than two.
func (v *Verifier) ask(ctx context.Context, guid, uuid string) (Identity, error) {
	payload, _ := json.Marshal(map[string]string{"id": guid})

	q := url.Values{}
	q.Set("guid", guid)
	q.Set("uuid", uuid)
	q.Set("method", "userview")
	q.Set("payload", string(payload))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.upstream+"?"+q.Encode(), nil)
	if err != nil {
		return Identity{}, err
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return Identity{}, fmt.Errorf("upstream unreachable: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Identity{}, err
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return Identity{}, ErrUnauthorised
	}
	if resp.StatusCode != http.StatusOK {
		return Identity{}, fmt.Errorf("upstream returned %d", resp.StatusCode)
	}

	// The live server prefixes its body with a blank line, and absent strings
	// come back as JSON null -- both established while building the client.
	var profile struct {
		GUID     *string `json:"guid"`
		Name     *string `json:"name"`
		Creation *string `json:"creation"`
	}
	if err := json.Unmarshal(trimLeadingSpace(body), &profile); err != nil {
		return Identity{}, fmt.Errorf("upstream sent unreadable JSON: %w", err)
	}

	// A 200 whose profile is for someone else would mean the pairing is not
	// enforced upstream after all. Refuse rather than trust it.
	if profile.GUID == nil || *profile.GUID != guid {
		return Identity{}, ErrUnauthorised
	}

	name := ""
	if profile.Name != nil {
		name = *profile.Name
	}

	// A date we cannot read must not cost anybody a login, so a failure here is
	// logged and dropped rather than returned.
	var created int64
	if profile.Creation != nil {
		if n, ok := v.parseCreation(*profile.Creation); ok {
			created = n
		} else if strings.TrimSpace(*profile.Creation) != "" {
			// The raw value is logged because it is the only way to learn the
			// format: TribesNext's own is unobserved, and the guess below is
			// this codebase's convention rather than a measurement. One line
			// naming the real shape beats a wrong date on every profile.
			v.log.Warn("unreadable creation date from upstream",
				"guid", guid, "creation", *profile.Creation)
		}
	}

	return Identity{GUID: guid, Name: name, Created: created}, nil
}

// parseCreation reads TribesNext's `creation` field as unix seconds.
//
// The format is a guess, and deliberately a narrow one. This codebase carries
// timestamps as decimal strings (store/user.go renders Creation that way), and
// model.go records that the original PHP returned database columns unconverted
// -- so a numeric string is the shape to expect. If theirs turns out to be a
// SQL datetime, this rejects it and the caller keeps the old behaviour.
//
// The band is what makes the rejection useful rather than decorative. Bare
// ParseInt would accept a millisecond timestamp (a REGISTERED date some
// fifty-six thousand years out), a negative, and the 0 an empty column decodes
// to -- each of which renders as a plausible-looking pane with a nonsense date
// in it, which is worse than falling back.
func (v *Verifier) parseCreation(raw string) (int64, bool) {
	// 2000-01-01. TribesNext did not exist before 2008 and Tribes 2 shipped in
	// 2001, so anything earlier is a decoding accident rather than a date.
	const earliest = 946684800

	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, false
	}
	// A day of slack for clock skew between here and upstream.
	if n < earliest || n > v.now().Unix()+86400 {
		return 0, false
	}
	return n, true
}

func trimLeadingSpace(b []byte) []byte {
	i := 0
	for i < len(b) && (b[i] == ' ' || b[i] == '\n' || b[i] == '\r' || b[i] == '\t') {
		i++
	}
	return b[i:]
}
