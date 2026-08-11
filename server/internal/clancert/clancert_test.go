package clancert

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// One key for the whole file. Generating a 2048-bit key costs a noticeable
// fraction of a second and nothing here depends on having a fresh one.
var testKey = func() *rsa.PrivateKey {
	k, err := Generate(KeyBits)
	if err != nil {
		panic(err)
	}
	return k
}()

// A record shaped like the real thing: a quad, a tribe count, and one tribe.
const testRecord = "Orange01\t[TT]\t0\t4510186\n1\nTest Tribe\t[TT]\t0\t7\t3\tGeneral"

func testSigner(t *testing.T) *Signer {
	t.Helper()
	return FromKey(testKey, 3, 30*time.Minute)
}

// The verification the game server performs, written out longhand against the
// TorqueScript it mirrors:
//
//	%sigSha = rsa_mod_exp(%sig, %e, %n);
//	while (strlen(%sigSha) < 40)
//	   %sigSha = "0" @ %sigSha;
//	%calcSha = sha1sum(getFieldS(%comCert, 0, 4));
//
// Check() is the reusable form of this, but it is code under test itself. This
// is the independent statement of what the engine will do, so a mistake in
// Check cannot make a broken certificate look correct.
func recoverDigest(t *testing.T, e, n, sig string) string {
	t.Helper()

	ei, ok := new(big.Int).SetString(e, 16)
	if !ok {
		t.Fatalf("exponent %q is not hex", e)
	}
	ni, ok := new(big.Int).SetString(n, 16)
	if !ok {
		t.Fatalf("modulus is not hex")
	}
	si, ok := new(big.Int).SetString(sig, 16)
	if !ok {
		t.Fatalf("signature %q is not hex", sig)
	}

	out := fmt.Sprintf("%x", new(big.Int).Exp(si, ei, ni))
	for len(out) < 40 {
		out = "0" + out
	}
	return out
}

func TestSignatureRecoversTheDigestTheEngineWillCompute(t *testing.T) {
	s := testSigner(t)
	now := time.Unix(1_700_000_000, 0)

	cert, err := s.Sign("4510186", testRecord, now)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	fields := strings.Split(cert, "\t")
	if len(fields) != 6 {
		t.Fatalf("want 6 fields, got %d: %q", len(fields), cert)
	}

	e, n := s.PublicHex()
	got := recoverDigest(t, e, n, fields[5])

	// sha1sum(getFieldS(cert, 0, 4)) -- fields 0 through 4, tabs intact, and
	// nothing else. Getting the preimage wrong is the failure mode that would
	// look like a working signature everywhere except in the game.
	sum := sha1.Sum([]byte(strings.Join(fields[:5], "\t")))
	want := hex.EncodeToString(sum[:])

	if got != want {
		t.Fatalf("recovered digest\n got %s\nwant %s", got, want)
	}
}

func TestSignatureIsFixedWidthAndLowerCase(t *testing.T) {
	s := testSigner(t)
	e, n := s.PublicHex()
	width := len(n)

	// Ten certificates, because the interesting case is the roughly one in
	// sixteen whose signature has a leading zero nibble -- exactly the case the
	// shipped DCE path mishandles.
	for i := 0; i < 10; i++ {
		cert, err := s.Sign(strconv.Itoa(4510186+i), testRecord, time.Unix(1_700_000_000, 0))
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		sig := strings.Split(cert, "\t")[5]
		if len(sig) != width {
			t.Fatalf("signature %d is %d hex chars, want %d", i, len(sig), width)
		}
		if sig != strings.ToLower(sig) {
			t.Fatalf("signature %d is not lower case", i)
		}
		if recoverDigest(t, e, n, sig) != recoverDigest(t, e, n, strings.TrimLeft(sig, "0")) {
			t.Fatalf("signature %d changes value when unpadded", i)
		}
	}
}

func TestFieldLayout(t *testing.T) {
	s := FromKey(testKey, 7, 45*time.Minute)
	now := time.Unix(1_700_000_000, 0)

	cert, err := s.Sign("4510186", testRecord, now)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	fields := strings.Split(cert, "\t")

	if fields[0] != "7" {
		t.Errorf("key id: got %q, want 7", fields[0])
	}
	if fields[1] != "1700000000" {
		t.Errorf("issued: got %q", fields[1])
	}
	if want := strconv.FormatInt(now.Add(45*time.Minute).Unix(), 10); fields[2] != want {
		t.Errorf("expire: got %q, want %q", fields[2], want)
	}
	if fields[3] != "4510186" {
		t.Errorf("guid: got %q", fields[3])
	}

	// The blob is hex because the record it carries contains both separators of
	// the string it travels inside.
	if strings.ContainsAny(fields[4], "\t\n") {
		t.Errorf("blob carries a separator: %q", fields[4])
	}
	back, err := Record(cert)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if back != testRecord {
		t.Errorf("record round trip:\n got %q\nwant %q", back, testRecord)
	}

	// One line. The mod reads it out of a reassembled buffer and a stray
	// newline would end the certificate early.
	if strings.Contains(cert, "\n") {
		t.Errorf("certificate spans lines: %q", cert)
	}
}

func TestCheckAcceptsWhatSignProduces(t *testing.T) {
	s := testSigner(t)
	now := time.Unix(1_700_000_000, 0)
	e, n := s.PublicHex()

	cert, err := s.Sign("4510186", testRecord, now)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := Check(e, n, cert, "4510186", now.Add(time.Minute)); err != nil {
		t.Fatalf("check: %v", err)
	}
}

func TestCheckRefusals(t *testing.T) {
	s := testSigner(t)
	now := time.Unix(1_700_000_000, 0)
	e, n := s.PublicHex()

	good, err := s.Sign("4510186", testRecord, now)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	fields := strings.Split(good, "\t")

	bend := func(i int, v string) string {
		out := append([]string(nil), fields...)
		out[i] = v
		return strings.Join(out, "\t")
	}

	// One character of the signature, flipped. Raw RSA has no structure to
	// check, so this must fail on the digest comparison and nothing else.
	bentSig := []byte(fields[5])
	if bentSig[len(bentSig)-1] == '0' {
		bentSig[len(bentSig)-1] = '1'
	} else {
		bentSig[len(bentSig)-1] = '0'
	}

	cases := []struct {
		name string
		cert string
		guid string
		when time.Time
	}{
		{"expired", good, "4510186", now.Add(31 * time.Minute)},
		{"issued for someone else", good, "9999999", now},
		{"bent signature", bend(5, string(bentSig)), "4510186", now},
		{"edited record", bend(4, hex.EncodeToString([]byte("Admin\t[HQ]\t0\t4510186\n0"))), "4510186", now},
		{"edited expiry", bend(2, strconv.FormatInt(now.Add(99*time.Hour).Unix(), 10)), "4510186", now},
		{"record is not hex", bend(4, "nothex"), "4510186", now},
		{"too few fields", strings.Join(fields[:5], "\t"), "4510186", now},
		{"too many fields", good + "\textra", "4510186", now},
		{"empty", "", "4510186", now},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := Check(e, n, c.cert, c.guid, c.when); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestCheckRefusesAnotherKeysSignature(t *testing.T) {
	mine := testSigner(t)
	theirsKey, err := Generate(1024) // smaller: this one only has to be wrong
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	theirs := FromKey(theirsKey, 3, 30*time.Minute)

	now := time.Unix(1_700_000_000, 0)
	cert, err := theirs.Sign("4510186", testRecord, now)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	e, n := mine.PublicHex()
	if err := Check(e, n, cert, "4510186", now); err == nil {
		t.Fatal("a certificate signed by a different key was accepted")
	}
}

func TestExpiryIsInclusiveOfTheLastSecond(t *testing.T) {
	s := testSigner(t)
	now := time.Unix(1_700_000_000, 0)
	e, n := s.PublicHex()

	cert, _ := s.Sign("4510186", testRecord, now)
	expire := now.Add(30 * time.Minute)

	// The mod's test is `currentEpochTime() > expire`, so the expiry second
	// itself is still valid. Asserted because the boundary is the difference
	// between a certificate and its replacement overlapping or leaving a gap.
	if err := Check(e, n, cert, "4510186", expire); err != nil {
		t.Errorf("rejected on the expiry second: %v", err)
	}
	if err := Check(e, n, cert, "4510186", expire.Add(time.Second)); err == nil {
		t.Error("accepted a second past expiry")
	}
}

func TestNilSignerIsTheUnconfiguredState(t *testing.T) {
	var s *Signer
	if _, err := s.Sign("4510186", testRecord, time.Now()); err != ErrNoKey {
		t.Fatalf("want ErrNoKey, got %v", err)
	}
	if s.KeyID() != 0 || s.ModSettings() != "" || s.Fingerprint() != "" || s.TTL() != 0 {
		t.Fatal("a nil signer should describe itself as empty rather than panic")
	}
}

func TestSignRefusesAGuidCarryingASeparator(t *testing.T) {
	s := testSigner(t)
	for _, guid := range []string{"4510186\t9999", "4510186\nx", ""} {
		if _, err := s.Sign(guid, testRecord, time.Now()); err == nil {
			t.Errorf("accepted guid %q", guid)
		}
	}
}

func TestLoadReadsWhatMarshalWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clan.pem")
	if err := os.WriteFile(path, MarshalPEM(testKey), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	s, err := Load(path, 4, time.Hour)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if s.KeyID() != 4 || s.TTL() != time.Hour {
		t.Fatalf("load lost its parameters: id=%d ttl=%v", s.KeyID(), s.TTL())
	}

	// Same key in, same key out: the public half a mod would be configured with
	// has to match the private half certificates are signed with.
	wantE, wantN := FromKey(testKey, 4, time.Hour).PublicHex()
	gotE, gotN := s.PublicHex()
	if gotE != wantE || gotN != wantN {
		t.Fatal("the loaded key is not the one that was written")
	}
}

func TestLoadRefusals(t *testing.T) {
	dir := t.TempDir()

	notPEM := filepath.Join(dir, "junk.pem")
	if err := os.WriteFile(notPEM, []byte("this is not a key\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tinyPath := filepath.Join(dir, "tiny.pem")
	tiny := undersizedKey(t)
	if err := os.WriteFile(tinyPath, MarshalPEM(tiny), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"no path":     "",
		"missing":     filepath.Join(dir, "absent.pem"),
		"not pem":     notPEM,
		"below floor": tinyPath,
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(path, 1, time.Hour); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

// undersizedKey builds a 512-bit key by hand, because crypto/rsa will no longer
// generate one -- and a key that small is exactly what Load's floor exists to
// turn away.
func undersizedKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()

	for {
		p, err := rand.Prime(rand.Reader, 256)
		if err != nil {
			t.Fatalf("prime: %v", err)
		}
		q, err := rand.Prime(rand.Reader, 256)
		if err != nil {
			t.Fatalf("prime: %v", err)
		}
		if p.Cmp(q) == 0 {
			continue
		}

		n := new(big.Int).Mul(p, q)
		phi := new(big.Int).Mul(new(big.Int).Sub(p, one), new(big.Int).Sub(q, one))
		d := new(big.Int).ModInverse(big.NewInt(65537), phi)
		if d == nil || n.BitLen() != 512 {
			continue
		}

		k := &rsa.PrivateKey{
			PublicKey: rsa.PublicKey{N: n, E: 65537},
			D:         d,
			Primes:    []*big.Int{p, q},
		}
		k.Precompute()
		return k
	}
}

var one = big.NewInt(1)

func TestModSettingsAreTheLinesAnOperatorPastes(t *testing.T) {
	s := FromKey(testKey, 2, time.Hour)
	e, n := s.PublicHex()

	want := "$TNBS::ClanKeyE[2] = \"" + e + "\";\n$TNBS::ClanKeyN[2] = \"" + n + "\";"
	if got := s.ModSettings(); got != want {
		t.Fatalf("mod settings:\n got %s\nwant %s", got, want)
	}
}

// A platform hands a key over as an environment variable and gives you nowhere
// to write a file, so the same value has to work both ways.
func TestParseAcceptsTheKeyItselfAsWellAsAPath(t *testing.T) {
	raw := MarshalPEM(testKey)

	if !IsPEM(string(raw)) {
		t.Fatal("PEM not recognised as key material")
	}
	for _, path := range []string{"/etc/tnbrowser/clan.pem", "clan.pem", ""} {
		if IsPEM(path) {
			t.Errorf("%q recognised as key material", path)
		}
	}

	s, err := Parse(raw, 5, time.Hour)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Same key whichever way it arrived: a deployment that switches from a file
	// to an environment variable must keep issuing certificates the game
	// servers already trust.
	wantE, wantN := FromKey(testKey, 5, time.Hour).PublicHex()
	gotE, gotN := s.PublicHex()
	if gotE != wantE || gotN != wantN {
		t.Fatal("parsing the PEM directly produced a different key")
	}
}
