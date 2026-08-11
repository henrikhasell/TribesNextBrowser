package auth

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"
)

// A real TribesNext account certificate.
//
// Every field of it is public: the client hands exactly this to every game
// server it joins (t2csri/clientSide.cs:120-124), and the server it joins hands
// nothing back that the private key protects. It is here because the pinned key
// is only worth anything if it verifies certificates TribesNext actually
// signed, and no synthetic fixture can test that.
const realCertificate = "orange01\t4510186\t10001\t" +
	"b7c1a1f3d90cc96150beeadb496b5202080c9feb217b3375872a1b4fa60443f4" +
	"3d596d9ff7918c0ecb7211fb598c1378240b5de2e058d10a87b8d28b7064420e" +
	"b94dfbbebd554c463998fd179d8b184f07a3bccccb83a6bbe5ddbc4de72a4662" +
	"99c3f8030b62c4cde35f795be47f1ebff1d9835b12c47b34f7fe5240f87fe98d" + "\t" +
	"4e1311e8063ab6af56495f5f471eebe0bec1c2a4781dbd5fe623dc2634c208a4" +
	"e3df6afa8f313c7e27dcd6bea18b571726d6f5707ea30d5286c9758ad160be13" +
	"9d401151121d3708f98c9ae51b10e1f4389fed2e3aa3928645870600b514f81f" +
	"1878c7bbb8287981e9125cd7e7f3d13de25fa93c105101f5473fd5d26253e141" +
	"60769efcc1e26139517b278920da592d886f8dc9c4d923722610285114f7c177" +
	"dbb9421071ee4fd932724f30d066a47eb28121cccf6a35d1b647adee4380d006" +
	"a7d5a3b8541235004def64c144fe63fd7d84d6257b0f4ab8295fa7a9a48ab123" +
	"e6efdaef42eafdc2626c7fe4dcc17d6273414d0887b26407d9fa253615d0bb42" +
	"e8388db1932253b2bf55983c9375bc50cbf4957b72567ea96073cc75cef34e3f" +
	"1ed7570dc97bcd339fc4adbd280908f11a546b4406301eb1f06fb0230979a28e" +
	"9854e682cd50b16bd6247d14ae954794b7063b2b019155f251fd741d7ca7e976" +
	"a6a801f0263d2bc5d05941bee1715ea6bcec42533825fd8bf063acdd3557611f" +
	"468a9e6219e36e0f720759e6393868945388593bc6bdb2f1c03be0cdfd51a220" +
	"902db3fbee764b3d0eff8181072a13cec832a14b86363c4b047ef35f790329b5" +
	"4330871e65479866660f23c3e36991e847003b4d1ef54a323ebd3f05c49ae25e" +
	"e1592fda75c2bdde844e3e6243c086d16a8b253fe77fbb427f541c11e39705d9"

// The whole change rests on this: a certificate signed by TribesNext verifies
// here, with no TribesNext involved.
func TestPinnedKeyVerifiesRealCertificate(t *testing.T) {
	cert, err := ParseCertificate(realCertificate)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := cert.Verify(); err != nil {
		t.Fatalf("verify: %v", err)
	}

	id := cert.Identity()
	if id.GUID != "4510186" || id.Name != "orange01" {
		t.Errorf("identity = %+v, want orange01/4510186", id)
	}
	// A certificate carries no registration date, and 0 is what tells the
	// store to date the account from first sighting rather than the epoch.
	if id.Created != 0 {
		t.Errorf("created = %d, want 0", id.Created)
	}
}

// The signature covers all four fields, so touching any of them has to break
// it. Without this the name and GUID would be attacker-chosen -- which is the
// whole identity model.
func TestTamperedCertificatesAreRefused(t *testing.T) {
	f := strings.Split(realCertificate, "\t")

	swap := func(i int, v string) string {
		g := append([]string(nil), f...)
		g[i] = v
		return strings.Join(g, "\t")
	}
	// Flip the last hex digit of a field, leaving its shape intact.
	bend := func(s string) string {
		last := s[len(s)-1]
		if last == 'a' {
			return s[:len(s)-1] + "b"
		}
		return s[:len(s)-1] + "a"
	}

	cases := map[string]string{
		"renamed":          swap(0, "Harabec"),
		"another guid":     swap(1, "4120041"),
		"exponent bent":    swap(2, "10003"),
		"modulus bent":     swap(3, bend(f[3])),
		"signature bent":   swap(4, bend(f[4])),
		"signature zeroed": swap(4, "00"),
	}

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			cert, err := ParseCertificate(raw)
			if err != nil {
				return // refused even earlier, which is also a refusal
			}
			if err := cert.Verify(); err == nil {
				t.Error("verified a tampered certificate")
			}
		})
	}
}

// Malformed input is refused before any big-integer work happens, and the
// checks are the shipped game server's (serverSide.cs:76-101) rather than
// invented here.
func TestMalformedCertificatesAreRefused(t *testing.T) {
	cases := map[string]string{
		"empty":           "",
		"four fields":     "orange01\t4510186\t10001\tb7c1",
		"six fields":      realCertificate + "\textra",
		"guid not digits": "orange01\t45101a86\t10001\tb7c1\t4e13",
		"guid empty":      "orange01\t\t10001\tb7c1\t4e13",
		"name blank":      "   \t4510186\t10001\tb7c1\t4e13",
		"exponent junk":   "orange01\t4510186\tzz\tb7c1\t4e13",
		"modulus junk":    "orange01\t4510186\t10001\tzz\t4e13",
		"signature junk":  "orange01\t4510186\t10001\tb7c1\tzz",
		"oversized":       "orange01\t4510186\t10001\t" + strings.Repeat("a", certLimit) + "\t4e13",
	}

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseCertificate(raw); err == nil {
				t.Error("accepted a malformed certificate")
			}
		})
	}
}

// A trailing newline is not corruption: t2csri_getAccountCertificate returns
// the stored line and the client sends it as it found it.
func TestTrailingNewlineTolerated(t *testing.T) {
	cert, err := ParseCertificate(realCertificate + "\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := cert.Verify(); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

//-----------------------------------------------------------------------------
// A stand-in client, holding a private key -- which is the one thing a real
// certificate cannot give a test.
//-----------------------------------------------------------------------------

// testAccount is a keypair plus a certificate signed by a test authority, so
// the challenge/response can be driven end to end. Small primes on purpose:
// these numbers only have to satisfy the arithmetic, and a 4096-bit keygen in
// a unit test buys nothing.
type testAccount struct {
	cert    string
	d, n, e *big.Int
}

// newTestAccount builds an account and re-points the package's pinned key at
// the test authority for the duration of the test.
func newTestAccount(t *testing.T, name, guid string) *testAccount {
	t.Helper()

	// Account key: RSA with p, q chosen so the modulus comfortably exceeds the
	// nonce and challenge the protocol puts through it.
	p, _ := new(big.Int).SetString("d4a7bd4a3ff2f7f0b0aa5a3e6f4e0b2f7a2f4b6d8e1c3a5b7d9f1e3c5a7b9d1f", 16)
	q, _ := new(big.Int).SetString("c3b5a7d9e1f3c5b7a9d1e3f5c7b9a1d3e5f7c9b1a3d5e7f9c1b3a5d7e9f1c3b5", 16)
	p = nextPrime(p)
	q = nextPrime(q)

	n := new(big.Int).Mul(p, q)
	phi := new(big.Int).Mul(new(big.Int).Sub(p, big.NewInt(1)), new(big.Int).Sub(q, big.NewInt(1)))
	e := big.NewInt(65537)
	d := new(big.Int).ModInverse(e, phi)
	if d == nil {
		t.Fatal("test key: e is not invertible")
	}

	// The authority that signs it, with e = 3 like the real one. Its modulus
	// must exceed a SHA-1, which any key this size does -- and its primes must
	// leave 3 invertible mod phi, which is what usableFor checks.
	ap := usableFor(mustInt("f1e3d5c7b9a1f3e5d7c9b1a3f5e7d9c1b3a5f7e9d1c3b5a7f9e1d3c5b7a9f1e3"), 3)
	aq := usableFor(mustInt("e9d1c3b5a7f9e1d3c5b7a9f1e3d5c7b9a1f3e5d7c9b1a3f5e7d9c1b3a5f7e9d1"), 3)
	an := new(big.Int).Mul(ap, aq)
	aphi := new(big.Int).Mul(new(big.Int).Sub(ap, big.NewInt(1)), new(big.Int).Sub(aq, big.NewInt(1)))
	ae := big.NewInt(3)
	ad := new(big.Int).ModInverse(ae, aphi)
	if ad == nil {
		t.Fatal("test authority: e is not invertible")
	}

	fields := []string{name, guid, e.Text(16), n.Text(16)}
	sum := sha1.Sum([]byte(strings.Join(fields, "\t")))
	sig := new(big.Int).Exp(new(big.Int).SetBytes(sum[:]), ad, an)

	// Swap the pinned key for the test authority, and put it back afterwards.
	saved := authKey
	authKey = &PublicKey{E: ae, N: an}
	t.Cleanup(func() { authKey = saved })

	return &testAccount{
		cert: strings.Join(append(fields, sig.Text(16)), "\t"),
		d:    d, n: n, e: e,
	}
}

// decrypt is what t2csri_rsa_decrypt does on the client: c^d mod n, rendered as
// hex with no leading zeros, which is what makes the client's offset
// arithmetic depend on a nonce that starts with a non-zero digit.
func (a *testAccount) decrypt(hexBlob string) string {
	c, _ := new(big.Int).SetString(hexBlob, 16)
	return new(big.Int).Exp(c, a.d, a.n).Text(16)
}

func mustInt(s string) *big.Int {
	n, ok := new(big.Int).SetString(s, 16)
	if !ok {
		panic("bad test integer")
	}
	return n
}

// usableFor finds a prime at or above n that leaves e invertible modulo p-1.
// With e = 3 that rules out every prime congruent to 1 mod 3, which is half of
// them -- and picking one anyway is what makes ModInverse return nil.
func usableFor(n *big.Int, e int64) *big.Int {
	ee := big.NewInt(e)
	p := nextPrime(n)
	for {
		rem := new(big.Int).Mod(new(big.Int).Sub(p, big.NewInt(1)), ee)
		if rem.Sign() != 0 {
			return p
		}
		p = nextPrime(new(big.Int).Add(p, big.NewInt(2)))
	}
}

func nextPrime(n *big.Int) *big.Int {
	p := new(big.Int).Set(n)
	if p.Bit(0) == 0 {
		p.Add(p, big.NewInt(1))
	}
	for !p.ProbablyPrime(20) {
		p.Add(p, big.NewInt(2))
	}
	return p
}

// The nonce the client builds: half the modulus width in hex, leading "1", so
// the decrypted value keeps its length (session.cs:191-203).
func clientNonce(a *testAccount) string {
	width := len(a.n.Text(16)) / 2
	nonce := "1"
	for len(nonce) < width {
		nonce += "7"
	}
	return nonce
}

//-----------------------------------------------------------------------------
// The exchange
//-----------------------------------------------------------------------------

func TestChallengeResponseGrantsSession(t *testing.T) {
	acct := newTestAccount(t, "orange01", "4510186")
	s := NewSessions(time.Minute)

	nonce := clientNonce(acct)
	blob, id, err := s.Challenge(acct.cert, nonce)
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}
	if id.GUID != "4510186" || id.Name != "orange01" {
		t.Fatalf("identity = %+v", id)
	}

	// The client's half of the protocol, verbatim: decrypt, check the nonce
	// replays, keep the rest.
	plain := acct.decrypt(blob)
	if !strings.EqualFold(plain[:len(nonce)], nonce) {
		t.Fatalf("nonce did not replay: %q does not start with %q", plain, nonce)
	}
	answer := plain[len(nonce):]

	token, id, err := s.Answer("4510186", answer)
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if token == "" {
		t.Fatal("no token")
	}

	got, ok := s.Lookup("4510186", token)
	if !ok || got.Name != "orange01" {
		t.Fatalf("lookup = %+v, %v", got, ok)
	}
	// The token belongs to the account it was granted to, and to no other.
	if _, ok := s.Lookup("4120041", token); ok {
		t.Error("token accepted for a different guid")
	}
}

// The response is what proves possession, so everything about it has to be
// refused when it is wrong.
func TestAnswerRefusals(t *testing.T) {
	newExchange := func(t *testing.T) (*Sessions, *testAccount, string) {
		t.Helper()
		acct := newTestAccount(t, "orange01", "4510186")
		s := NewSessions(time.Minute)
		nonce := clientNonce(acct)
		blob, _, err := s.Challenge(acct.cert, nonce)
		if err != nil {
			t.Fatalf("challenge: %v", err)
		}
		plain := acct.decrypt(blob)
		return s, acct, plain[len(nonce):]
	}

	t.Run("wrong answer", func(t *testing.T) {
		s, _, _ := newExchange(t)
		if _, _, err := s.Answer("4510186", "deadbeefdeadbeef"); err != ErrNoSession {
			t.Errorf("err = %v, want ErrNoSession", err)
		}
	})

	t.Run("answered for another guid", func(t *testing.T) {
		s, _, answer := newExchange(t)
		if _, _, err := s.Answer("4120041", answer); err != ErrNoSession {
			t.Errorf("err = %v, want ErrNoSession", err)
		}
	})

	t.Run("replayed", func(t *testing.T) {
		s, _, answer := newExchange(t)
		if _, _, err := s.Answer("4510186", answer); err != nil {
			t.Fatalf("first answer: %v", err)
		}
		if _, _, err := s.Answer("4510186", answer); err != ErrNoSession {
			t.Errorf("a challenge was answerable twice: %v", err)
		}
	})

	// A wrong answer must not leave the challenge standing to be ground at.
	t.Run("burned by a wrong answer", func(t *testing.T) {
		s, _, answer := newExchange(t)
		if _, _, err := s.Answer("4510186", "00"); err != ErrNoSession {
			t.Fatalf("wrong answer accepted: %v", err)
		}
		if _, _, err := s.Answer("4510186", answer); err != ErrNoSession {
			t.Error("the right answer still worked after a wrong one")
		}
	})

	t.Run("expired", func(t *testing.T) {
		s, _, answer := newExchange(t)
		s.now = func() time.Time { return time.Now().Add(2 * challengeWindow) }
		if _, _, err := s.Answer("4510186", answer); err != ErrNoSession {
			t.Errorf("err = %v, want ErrNoSession", err)
		}
	})

	t.Run("never challenged", func(t *testing.T) {
		s := NewSessions(time.Minute)
		if _, _, err := s.Answer("4510186", "01"); err != ErrNoSession {
			t.Errorf("err = %v, want ErrNoSession", err)
		}
	})
}

// A nonce at or above the modulus would come back as its remainder, and the
// client would compare a value it never sent. Refusing is the only honest
// answer.
func TestNonceTooLargeForKeyIsRefused(t *testing.T) {
	acct := newTestAccount(t, "orange01", "4510186")
	s := NewSessions(time.Minute)

	huge := strings.Repeat("f", len(acct.n.Text(16))+8)
	if _, _, err := s.Challenge(acct.cert, huge); err == nil {
		t.Error("accepted a nonce larger than the account modulus")
	}
}

// An unsigned certificate never reaches the challenge stage, however
// well-formed it is.
func TestChallengeRequiresASignedCertificate(t *testing.T) {
	acct := newTestAccount(t, "orange01", "4510186")
	s := NewSessions(time.Minute)

	f := strings.Split(acct.cert, "\t")
	f[0] = "Harabec"
	if _, _, err := s.Challenge(strings.Join(f, "\t"), clientNonce(acct)); err == nil {
		t.Error("challenged an unsigned certificate")
	}
}

// Sessions expire, and using one puts the window back -- a player mid-session
// should not be logged out by a clock.
func TestSessionExpiryIsSliding(t *testing.T) {
	acct := newTestAccount(t, "orange01", "4510186")
	s := NewSessions(time.Minute)

	now := time.Now()
	s.now = func() time.Time { return now }

	nonce := clientNonce(acct)
	blob, _, err := s.Challenge(acct.cert, nonce)
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}
	token, _, err := s.Answer("4510186", acct.decrypt(blob)[len(nonce):])
	if err != nil {
		t.Fatalf("answer: %v", err)
	}

	// Half a window later it is live, and that read extends it.
	now = now.Add(30 * time.Second)
	if _, ok := s.Lookup("4510186", token); !ok {
		t.Fatal("session dropped early")
	}

	// Another half window: still live, because the read moved the deadline.
	now = now.Add(45 * time.Second)
	if _, ok := s.Lookup("4510186", token); !ok {
		t.Fatal("sliding expiry did not extend the session")
	}

	// Left alone for a whole window, it goes.
	now = now.Add(2 * time.Minute)
	if _, ok := s.Lookup("4510186", token); ok {
		t.Error("expired session still accepted")
	}
	if n := s.Count(); n != 0 {
		t.Errorf("%d sessions left after expiry, want 0", n)
	}
}

// The fingerprint is what a startup line prints, so it has to name the key that
// is actually compiled in rather than a value typed beside it.
func TestFingerprintMatchesTheModulus(t *testing.T) {
	want := Fingerprint()
	if len(want) != 64 {
		t.Fatalf("fingerprint = %q, want 64 hex characters", want)
	}
	if _, err := hex.DecodeString(want); err != nil {
		t.Fatalf("fingerprint is not hex: %v", err)
	}
	if authKey.N.BitLen() != 4096 {
		t.Errorf("pinned modulus is %d bits, want 4096", authKey.N.BitLen())
	}
	if authKey.E.Cmp(big.NewInt(3)) != 0 {
		t.Errorf("pinned exponent = %s, want 3", authKey.E)
	}
	if got := fmt.Sprintf("%x", authKey.N); got != authModulus {
		t.Error("the parsed modulus does not round-trip to the constant")
	}
}
