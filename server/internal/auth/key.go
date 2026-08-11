package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
)

// TribesNext's account-signing key, and the reason this server needs nobody's
// permission to verify a player.
//
// Every TribesNext account certificate is signed with it, and every game server
// checks that signature offline against the same key compiled into IFC22.dll
// (t2csri/serverSide.cs:103-128). Holding the public half here buys exactly what
// it buys them: the ability to tell a real account from an invented one without
// asking anyone.
//
// # Where this came from
//
// Recovered arithmetically from certificates, not extracted from the DLL -- the
// signature scheme is raw RSA over a bare SHA-1, which makes the modulus fall
// out of any few certificates:
//
//	m_i   = sha1(name \t guid \t e \t n)      as an integer
//	sig_i^E ≡ m_i (mod N)                     so N divides sig_i^E - m_i
//	N     = gcd over i, small factors removed
//
// With E = 3 and three certificates that gives N exactly. To re-derive it rather
// than trust the constant below, run that computation over any public.store --
// the file the client hands to every game server it joins -- and compare
// Fingerprint().
//
// Verified against five certificates from two independent installs at the time
// of writing. TestPinnedKeyVerifiesRealCertificates keeps that honest.
const (
	// authExponent is small by modern standards and is not ours to change: it
	// is what the signatures were made with.
	authExponent = 3

	// authModulus is 4096 bits, lower-case hex, no leading zeros.
	authModulus = "" +
		"a2303d7c7d3ff28daa2dfd4f7fddcbb939aa461a3a298f22f4cbc8f87c6e5844" +
		"79861b1a638ea2daf87a18420f65b486cbe70453ce4f5f38a55b30026457eab3" +
		"ef2716aa409d25cb8b809a9ef06e3bcdc5b37f0d975cca972601d6a6d4be10e8" +
		"e5994708e878ab846caa58466eea79ccd1b49f9f06e4d6b335ba2b47b6dc81a0" +
		"4167a2c2fa2b9f847c1375cb6fef1a3c7ced04f750a9605fab584d2d2a4bbed6" +
		"d41d4c2eeb5e223ac3bc6d8ac025f4b25bb1058998f768025baea18272a3bdda" +
		"b58f94a9cb4a14452eaf7a50812e012fc42feb5d986183da134f168b447e4da6" +
		"e00c351bc679b098b78bfdf3e17efcd65ec3d2098a14426957db5e30fa012929" +
		"61f3caf3aadb12aff45782dd9f8737953491c90ad4df8b76ef2559b4ef3bae95" +
		"7f964cf4c9ded3d2171c9d190ba5a6a079a3d75f2c583217db626bd4074a2559" +
		"794b475a9c2384096aa2b4816d2e61c144d1de14164b77428a5329da3a4a8174" +
		"b6a84d8733495ac6bc7efa981f23b4f401a6216e18771856721c74ff136c1cbe" +
		"7449de0dff26c04f940d3ce31ef369e3b09932ffe7ffb347554bd2171dc5c793" +
		"37a6d0dad0f8aa5f45bf054d0d0e5b73eaa83e6fdcaf4cbf2422697aa570823c" +
		"682d76087b6815b0212c80e3ed550420a45f70533214619efc468548af45b6e7" +
		"3fec0acbae55b08c978c0c3de09c79a50626502c49b541b7bf002508cc5a8095"
)

// PublicKey is an RSA public key in the form this protocol carries them:
// hex-encoded exponent and modulus, with no padding scheme and no ASN.1 around
// them. crypto/rsa is deliberately not used -- it would refuse these outright,
// since every signature here is a bare SHA-1 with no PKCS#1 structure and the
// account keys are far below its minimum size.
type PublicKey struct {
	E *big.Int
	N *big.Int
}

// ParsePublicKey reads the hex pair a certificate carries.
func ParsePublicKey(e, n string) (*PublicKey, error) {
	ei, ok := parseHex(e)
	if !ok {
		return nil, fmt.Errorf("exponent is not hex")
	}
	ni, ok := parseHex(n)
	if !ok {
		return nil, fmt.Errorf("modulus is not hex")
	}
	if ei.Sign() <= 0 || ni.Sign() <= 0 {
		return nil, fmt.Errorf("key must be positive")
	}
	return &PublicKey{E: ei, N: ni}, nil
}

// Apply is the raw operation both directions of this protocol are built from:
// m^e mod n. Verifying a signature and encrypting a challenge are the same
// call, which is a property of the scheme rather than a shortcut here.
func (k *PublicKey) Apply(m *big.Int) *big.Int {
	return new(big.Int).Exp(m, k.E, k.N)
}

// authKey is the pinned key, parsed once.
var authKey = mustKey(authExponent, authModulus)

// Fingerprint identifies the pinned key without printing 1024 hex characters of
// it: sha256 of the lower-case modulus hex. Startup logs it so an operator can
// tell at a glance which key a build carries.
func Fingerprint() string {
	sum := sha256.Sum256([]byte(strings.ToLower(authModulus)))
	return hex.EncodeToString(sum[:])
}

func mustKey(e int64, n string) *PublicKey {
	k, err := ParsePublicKey(fmt.Sprintf("%x", e), n)
	if err != nil {
		panic("auth: pinned key is unparseable: " + err.Error())
	}
	return k
}

// parseHex reads a hex string as a big integer.
//
// Strict about the alphabet and lax about width: the client zero-pads its hex
// for display (session.cs) and the engine's bignums strip leading zeros on the
// way back, so any length is legitimate and only the value means anything.
func parseHex(s string) (*big.Int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return nil, false
		}
	}
	n, ok := new(big.Int).SetString(s, 16)
	return n, ok
}
