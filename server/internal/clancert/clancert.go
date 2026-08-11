// Package clancert issues the certificate that puts a player's clan tag on
// their name in game.
//
// # Why a certificate and not a lookup
//
// A game server builds the displayed name from GameConnection::getAuthInfo(),
// and it does so inside onConnect, synchronously. The mod used to fill that
// record by asking this server over HTTP while holding the connection open. A
// signed certificate the player carries removes the round trip, the cache and
// the hold: the game server checks it offline, the way it already checks the
// account certificate TribesNext signed.
//
// # Why not the shipped community certificate
//
// TribesNext has this feature, and its verifier is dead: a community
// certificate is verified with a delegated server's key, that server's own
// certificate is signed by a root key compiled into IFC22.dll, and the only
// certificate ever issued has expired. Nobody but its author can renew it.
//
// We do not need that chain. It exists so an arbitrary community server can
// convince an arbitrary game server with nothing configured between them; we
// have one issuer, and every verifier already holds our key because the
// verifier is our mod. What we cannot self-assert -- that a GUID belongs to a
// real account with a real name -- is exactly what the TribesNext account
// certificate already says, and the game server has checked it before ours is
// looked at. So this certificate only has to bind a clan record to a GUID that
// somebody else established.
//
// # The format
//
//	KeyID <TAB> Issued <TAB> Expire <TAB> GUID <TAB> HexBlob <TAB> Sig
//
// Six tab-separated fields on one line. HexBlob is the hex of the auth-info
// record dbproxy.Certificate produces -- hex because the record itself contains
// tabs and newlines, which are the field and record separators of the string it
// has to travel inside.
//
// Sig is raw RSA over a bare SHA-1 of fields 0..4 joined with tabs: no PKCS#1,
// no ASN.1, no hash prefix. That is not a choice, it is what the verifier does.
// The mod checks it with the engine's rsa_mod_exp(sig, e, n) and compares the
// result to sha1sum(preimage) as hex strings, so the number being signed is the
// integer whose hex representation is the digest.
//
// # Lifetime
//
// Short, and it is the only freshness mechanism there is -- no revocation
// exists, so expiry is also how a player who leaves a clan stops wearing its
// tag. It can be aggressive because an expired certificate costs a tag and
// nothing else: the mod applies what verifies and leaves the player alone
// otherwise. Nothing here can stop somebody playing.
package clancert

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"
)

// ErrNoKey is what every entry point answers when the server was started
// without a signing key. Callers turn it into a 404: a deployment that has not
// configured this simply does not offer it, and must still start and serve
// everything else.
var ErrNoKey = fmt.Errorf("no clan signing key configured")

// DefaultTTL is how long an issued certificate stays valid.
//
// Half an hour. The client refreshes at half-life, so a player picks up a clan
// change within about fifteen minutes, and a player who left one keeps the tag
// for at most thirty. Both numbers are cosmetic-only, which is what allows them
// to be this short: nothing is disconnected when a certificate lapses.
//
// There is a floor under this that is not ours. The game server compares
// epoch seconds in TorqueScript, which evaluates numbers as 32-bit floats, so
// timestamps this large quantise to multiples of 128 -- measured in game, where
// a 1800-second lifetime reports itself as 1792. Anything under a few minutes
// would be decided by rounding rather than by time.
const DefaultTTL = 30 * time.Minute

// KeyBits is the size Generate produces.
//
// 2048, against the 4096 TribesNext uses for accounts. The verifier is a modexp
// run inside a script callback on the game server's connect path, once per
// joining player, and this halves both that and the bytes the client has to
// chunk across the command channel. It signs a clan tag, not an identity.
const KeyBits = 2048

// Signer issues certificates. A nil *Signer is the "no key configured" state
// and answers ErrNoKey rather than panicking, so callers need no second flag.
type Signer struct {
	keyID int
	ttl   time.Duration
	key   *rsa.PrivateKey
}

// Load reads a PEM private key from a file.
func Load(path string, keyID int, ttl time.Duration) (*Signer, error) {
	if path == "" {
		return nil, ErrNoKey
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading the clan signing key: %w", err)
	}
	return Parse(raw, keyID, ttl)
}

// IsPEM reports whether a configured value is key material rather than a path
// to some.
//
// Both are supported because both are how a key actually arrives. A machine you
// administer has a file; a platform hands you an environment variable and no
// filesystem to write to -- App Platform runs this from a distroless image with
// no volume, so a path there would have nothing to point at.
func IsPEM(s string) bool {
	return strings.Contains(s, "-----BEGIN")
}

// Parse reads a PEM private key.
//
// Both PKCS#1 ("RSA PRIVATE KEY", what openssl genrsa writes) and PKCS#8
// ("PRIVATE KEY", what most modern tooling writes) are accepted, because
// whichever one an operator happens to produce is the one they will try.
func Parse(raw []byte, keyID int, ttl time.Duration) (*Signer, error) {
	if keyID < 0 {
		return nil, fmt.Errorf("key id must not be negative")
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}

	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("the clan signing key is not PEM")
	}

	var key *rsa.PrivateKey
	switch k, err := x509.ParsePKCS1PrivateKey(block.Bytes); {
	case err == nil:
		key = k
	default:
		any, err8 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err8 != nil {
			return nil, fmt.Errorf("the clan signing key is neither PKCS#1 nor PKCS#8 RSA: %w", err)
		}
		k8, ok := any.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("the clan signing key is not an RSA key")
		}
		key = k8
	}

	if err := key.Validate(); err != nil {
		return nil, fmt.Errorf("the clan signing key is malformed: %w", err)
	}
	// There is no padding scheme here to compensate for a small modulus, and a
	// forged clan certificate is a forged identity as far as the scoreboard is
	// concerned. 1024 is the floor; Generate makes 2048.
	if key.N.BitLen() < 1024 {
		return nil, fmt.Errorf("the clan signing key is only %d bits; use at least 1024", key.N.BitLen())
	}
	return &Signer{keyID: keyID, ttl: ttl, key: key}, nil
}

// FromKey wraps an already-parsed key. Tests use it; Load is the real path.
func FromKey(key *rsa.PrivateKey, keyID int, ttl time.Duration) *Signer {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Signer{keyID: keyID, ttl: ttl, key: key}
}

// Generate makes a fresh signing key.
func Generate(bits int) (*rsa.PrivateKey, error) {
	if bits <= 0 {
		bits = KeyBits
	}
	return rsa.GenerateKey(rand.Reader, bits)
}

// MarshalPEM encodes a key for -genkey to write out. PKCS#1, because that is
// what an operator inspecting it with openssl will expect to see.
func MarshalPEM(key *rsa.PrivateKey) []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}

// Sign issues a certificate for one player.
//
// record is the auth-info blob from dbproxy.Certificate, passed through
// unmodified: this function asserts that record, it does not decide it.
func (s *Signer) Sign(guid, record string, now time.Time) (string, error) {
	if s == nil {
		return "", ErrNoKey
	}
	if guid == "" {
		return "", fmt.Errorf("no guid to issue for")
	}
	// A tab or a newline in the GUID would move a field boundary and let one
	// certificate be read as another. Nothing upstream can produce that today;
	// this is here so nothing downstream has to assume it.
	if strings.ContainsAny(guid, "\t\n") {
		return "", fmt.Errorf("guid contains a separator")
	}

	fields := []string{
		strconv.Itoa(s.keyID),
		strconv.FormatInt(now.Unix(), 10),
		strconv.FormatInt(now.Add(s.ttl).Unix(), 10),
		guid,
		hex.EncodeToString([]byte(record)),
	}

	sig := new(big.Int).Exp(digest(strings.Join(fields, "\t")), s.key.D, s.key.N)

	// Fixed width, leading zeros intact. The value is all the verifier reads,
	// but a constant-length signature keeps the certificate a constant size,
	// which is one less thing to be surprised by when it is chunked across the
	// command channel.
	return strings.Join(append(fields, pad(sig, hexWidth(s.key.N))), "\t"), nil
}

// KeyID is the number stamped into field 0, which is how a game server picks
// the public key to check a certificate with.
func (s *Signer) KeyID() int {
	if s == nil {
		return 0
	}
	return s.keyID
}

// PublicHex is the key as the mod carries it: exponent and modulus in
// lower-case hex, no padding scheme and no ASN.1 around them.
func (s *Signer) PublicHex() (e, n string) {
	if s == nil {
		return "", ""
	}
	return fmt.Sprintf("%x", s.key.E), fmt.Sprintf("%x", s.key.N)
}

// ModSettings is the pair of TorqueScript lines a server operator puts in the
// mod, printed by -genkey and at startup.
//
// Handing over the exact lines rather than the numbers is deliberate: the
// alternative is an operator transcribing 512 hex characters into a settings
// file and finding out it was wrong when tags silently do not appear.
func (s *Signer) ModSettings() string {
	if s == nil {
		return ""
	}
	e, n := s.PublicHex()
	return fmt.Sprintf("$TNBS::ClanKeyE[%d] = \"%s\";\n$TNBS::ClanKeyN[%d] = \"%s\";",
		s.keyID, e, s.keyID, n)
}

// Fingerprint identifies the key without printing it: sha256 of the modulus
// hex, matching what auth.Fingerprint does for the account key. Startup logs it
// so an operator can tell which key a deployment is signing with.
func (s *Signer) Fingerprint() string {
	if s == nil {
		return ""
	}
	_, n := s.PublicHex()
	sum := sha256.Sum256([]byte(n))
	return hex.EncodeToString(sum[:])
}

// TTL is the lifetime issued certificates get.
func (s *Signer) TTL() time.Duration {
	if s == nil {
		return 0
	}
	return s.ttl
}

//-----------------------------------------------------------------------------
// The verifier, as the mod performs it
//-----------------------------------------------------------------------------

// Check is the game server's verification, in Go.
//
// It exists to be tested. TNBrowserServer/tnbserver/clancert.cs is the real
// verifier and this mirrors it step for step -- field count, the recovered
// digest compared as 40 lower-case hex characters, expiry, and the GUID the
// account certificate established. If the two ever disagree, one of them is
// wrong and the Go tests are where that becomes visible.
func Check(e, n, cert, guid string, now time.Time) error {
	fields := strings.Split(cert, "\t")
	if len(fields) != 6 {
		return fmt.Errorf("want 6 fields, got %d", len(fields))
	}

	ei, ok := new(big.Int).SetString(strings.TrimSpace(e), 16)
	if !ok {
		return fmt.Errorf("exponent is not hex")
	}
	ni, ok := new(big.Int).SetString(strings.TrimSpace(n), 16)
	if !ok {
		return fmt.Errorf("modulus is not hex")
	}
	sig, ok := new(big.Int).SetString(strings.TrimSpace(fields[5]), 16)
	if !ok {
		return fmt.Errorf("signature is not hex")
	}

	// rsa_mod_exp(sig, e, n), left-padded to 40, against sha1sum(fields 0..4).
	//
	// The padding is not decoration. The shipped account path does it
	// (t2csri/serverSide.cs:114-115) and the shipped DCE path forgets to
	// (serverSideClans.cs:73-78), which would fail one certificate in sixteen --
	// the ones whose digest starts with a zero nibble.
	recovered := pad(new(big.Int).Exp(sig, ei, ni), 40)
	if recovered != pad(digest(strings.Join(fields[:5], "\t")), 40) {
		return fmt.Errorf("signature does not match")
	}

	expire, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return fmt.Errorf("expiry is not a number")
	}
	if now.Unix() > expire {
		return fmt.Errorf("certificate expired")
	}

	if fields[3] != guid {
		return fmt.Errorf("certificate is for %q, not %q", fields[3], guid)
	}

	if _, err := hex.DecodeString(fields[4]); err != nil {
		return fmt.Errorf("record is not hex: %w", err)
	}
	return nil
}

// Record decodes the auth-info blob out of a certificate. Verification is
// Check's job; this only reads.
func Record(cert string) (string, error) {
	fields := strings.Split(cert, "\t")
	if len(fields) != 6 {
		return "", fmt.Errorf("want 6 fields, got %d", len(fields))
	}
	raw, err := hex.DecodeString(fields[4])
	if err != nil {
		return "", fmt.Errorf("record is not hex: %w", err)
	}
	return string(raw), nil
}

//-----------------------------------------------------------------------------

// digest is the number that gets signed: the SHA-1 of the preimage, read as the
// integer its hex representation denotes.
func digest(preimage string) *big.Int {
	sum := sha1.Sum([]byte(preimage))
	return new(big.Int).SetBytes(sum[:])
}

// pad renders a value as lower-case hex, at least width characters wide.
func pad(v *big.Int, width int) string {
	return fmt.Sprintf("%0*x", width, v)
}

// hexWidth is how many hex characters a value modulo n occupies.
func hexWidth(n *big.Int) int {
	return (n.BitLen() + 3) / 4
}
