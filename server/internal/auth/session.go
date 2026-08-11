package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
)

// Sessions turns a certificate into a token, by way of a challenge.
//
// A certificate proves TribesNext issued a name and GUID for a key. It proves
// nothing about who is sending it, because it is public -- the client hands it
// to every game server it joins. So the holder is made to decrypt something
// only the private half can read, which is the same construction the shipped
// game server uses on a connecting player (t2csri/serverSide.cs:157-176) and
// the same one the client already speaks to TribesNext's robot login.
//
//	client sends   certificate + a nonce of its own
//	we send back   (nonce || challenge) encrypted with the certificate's key
//	client sends   the challenge half, having decrypted it
//	we send back   a token
//
// Each half of that plaintext is load-bearing. The challenge proves possession
// to us. The nonce proves to the CLIENT that the reply was minted for this
// request rather than replayed at it -- session.cs:227-234 checks it and
// discards the challenge if it does not match, so leaving it out would break a
// client that is right to be suspicious.
type Sessions struct {
	ttl time.Duration

	mu         sync.Mutex
	challenges map[string]pending // by guid: at most one outstanding
	tokens     map[string]granted // by token

	// now and randRead are swappable so tests can pin both.
	now      func() time.Time
	randRead func([]byte) (int, error)
}

type pending struct {
	challenge *big.Int
	id        Identity
	expires   time.Time
}

type granted struct {
	id      Identity
	expires time.Time
}

// challengeWindow is how long a challenge stays answerable. The client answers
// on the next tick (session.cs:278 schedules the decrypt 200ms out), so this is
// generous by three orders of magnitude and still short enough that an
// unanswered challenge is gone long before it could be useful to anyone.
const challengeWindow = 60 * time.Second

// challengeBits matches the game server's own rand_challenge(): 64 bits, which
// is the size the client's nonce is sized to sit beside.
const challengeBits = 64

func NewSessions(ttl time.Duration) *Sessions {
	if ttl <= 0 {
		// The client pings every $TNB::SessionRefresh (10 minutes by default),
		// so anything shorter would expire sessions between keepalives.
		ttl = 30 * time.Minute
	}
	return &Sessions{
		ttl:        ttl,
		challenges: map[string]pending{},
		tokens:     map[string]granted{},
		now:        time.Now,
		randRead:   rand.Read,
	}
}

// ErrNoSession is every refusal a caller should report as 401. Anything else
// coming out of this package is a fault on our side.
var ErrNoSession = fmt.Errorf("no valid session")

// Challenge verifies a certificate and answers with the encrypted blob the
// holder must decrypt. The returned string is hex, ready to send.
func (s *Sessions) Challenge(rawCert, nonce string) (string, Identity, error) {
	cert, err := ParseCertificate(rawCert)
	if err != nil {
		return "", Identity{}, err
	}
	if err := cert.Verify(); err != nil {
		return "", Identity{}, err
	}

	nonce = strings.TrimSpace(nonce)
	if _, ok := parseHex(nonce); !ok {
		return "", Identity{}, fmt.Errorf("%w: nonce is not hex", ErrBadCertificate)
	}

	challenge, err := s.randomChallenge()
	if err != nil {
		return "", Identity{}, err
	}

	// The plaintext is the two hex strings concatenated, then read as one
	// number -- not the two numbers added or interleaved. That is what lets the
	// client take its half back off the front by string offset
	// (session.cs:227), and it is why the client's nonce starts with a literal
	// "1": a leading zero would be lost when the engine's bignum renders the
	// decrypted value and every offset after it would shift.
	plain, ok := new(big.Int).SetString(nonce+challenge, 16)
	if !ok {
		return "", Identity{}, fmt.Errorf("%w: nonce is not hex", ErrBadCertificate)
	}

	// Raw RSA is a permutation of the integers below the modulus and nothing
	// else: a plaintext at or above it comes back as its remainder, so the
	// client would decrypt something it never sent. Refuse instead, which is a
	// legible failure rather than an unanswerable challenge.
	if plain.Cmp(cert.Key.N) >= 0 {
		return "", Identity{}, fmt.Errorf("%w: nonce too long for this key", ErrBadCertificate)
	}

	id := cert.Identity()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	// One outstanding challenge per account: a second request replaces the
	// first rather than queueing, so a client that retries cannot leave a
	// trail of answerable challenges behind it.
	c, _ := new(big.Int).SetString(challenge, 16)
	s.challenges[cert.GUID] = pending{
		challenge: c,
		id:        id,
		expires:   s.now().Add(challengeWindow),
	}

	return strings.ToLower(cert.Key.Apply(plain).Text(16)), id, nil
}

// Answer takes the decrypted challenge half and grants a token.
func (s *Sessions) Answer(guid, response string) (string, Identity, error) {
	got, ok := parseHex(response)
	if !ok {
		return "", Identity{}, ErrNoSession
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()

	p, found := s.challenges[guid]
	if !found || s.now().After(p.expires) {
		return "", Identity{}, ErrNoSession
	}

	// Single use, right or wrong. A challenge that survived a failed answer
	// would let someone grind at it, and one that survived a successful answer
	// would be replayable by anyone who saw the response go past.
	delete(s.challenges, guid)

	// Numerically, because what comes back is whatever the engine's bignum
	// rendered -- any case, any width.
	if got.Cmp(p.challenge) != 0 {
		return "", Identity{}, ErrNoSession
	}

	token, err := s.randomToken()
	if err != nil {
		return "", Identity{}, err
	}
	s.tokens[token] = granted{id: p.id, expires: s.now().Add(s.ttl)}
	return token, p.id, nil
}

// Lookup resolves a token, and extends it. Sliding rather than absolute: a
// player who is using the service should not be logged out mid-session because
// a fixed window ran out, and every request already proves the token is live.
func (s *Sessions) Lookup(guid, token string) (Identity, bool) {
	if guid == "" || token == "" {
		return Identity{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	g, found := s.tokens[token]
	if !found || s.now().After(g.expires) {
		return Identity{}, false
	}
	// The token is bound to the account it was granted to. Without this a
	// stolen token could be presented alongside any GUID, and every ordinal
	// keys its writes on the GUID.
	if g.id.GUID != guid {
		return Identity{}, false
	}

	g.expires = s.now().Add(s.ttl)
	s.tokens[token] = g
	return g.id, true
}

// Grant mints a session for an identity established some other way, and answers
// the token.
//
// One caller, and it is the dev bypass: a client with no account subsystem
// cannot answer a challenge, so the test suites need a session they did not
// prove. Nothing on the serving path reaches this unless that switch is on.
func (s *Sessions) Grant(id Identity) (string, error) {
	token, err := s.randomToken()
	if err != nil {
		return "", err
	}
	s.GrantToken(token, id)
	return token, nil
}

// GrantToken records a session whose proof happened somewhere other than
// Challenge/Answer, under a token the caller chose.
//
// Nothing in the serving path calls this: a real session is only ever minted by
// answering a challenge. It exists for tests that need an authenticated caller
// without a private key to answer with -- which is every test above the auth
// package, since the pinned key means only TribesNext can sign a usable
// certificate.
func (s *Sessions) GrantToken(token string, id Identity) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[token] = granted{id: id, expires: s.now().Add(s.ttl)}
}

// End drops a token. Nothing calls it yet; it exists so a logout has somewhere
// to go without reaching into the map.
func (s *Sessions) End(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, token)
}

// Count reports live sessions, for logging and tests.
func (s *Sessions) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	return len(s.tokens)
}

// sweepLocked drops what has expired. Called on the paths that already hold the
// lock rather than on a timer: the maps only grow when someone logs in, so
// there is nothing to collect between logins.
func (s *Sessions) sweepLocked() {
	now := s.now()
	for k, v := range s.challenges {
		if now.After(v.expires) {
			delete(s.challenges, k)
		}
	}
	for k, v := range s.tokens {
		if now.After(v.expires) {
			delete(s.tokens, k)
		}
	}
}

func (s *Sessions) randomChallenge() (string, error) {
	b := make([]byte, challengeBits/8)
	if _, err := s.randRead(b); err != nil {
		return "", fmt.Errorf("no randomness available: %w", err)
	}
	// Fixed width, leading zeros intact: this half sits behind the nonce in one
	// hex string, so its length is what the client's offset arithmetic assumes.
	return hex.EncodeToString(b), nil
}

func (s *Sessions) randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := s.randRead(b); err != nil {
		return "", fmt.Errorf("no randomness available: %w", err)
	}
	return hex.EncodeToString(b), nil
}
