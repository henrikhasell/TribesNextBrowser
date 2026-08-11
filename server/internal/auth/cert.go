package auth

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
)

// A TribesNext account certificate, as the client hands it over.
//
// Five tab-separated fields, and the layout is not ours: it is what
// t2csri_getAccountCertificate() returns and what every game server parses
// (t2csri/serverSide.cs:73-90).
//
//	0  name    the warrior name TribesNext signed
//	1  guid    decimal digits
//	2  e       the account key's public exponent, hex
//	3  n       the account key's modulus, hex
//	4  sig     TribesNext's signature over fields 0..3, hex
type Certificate struct {
	Name string
	GUID string
	Key  *PublicKey

	// Raw keeps the fields as they arrived. The signature covers the exact
	// bytes, so verification has to hash what was sent rather than anything
	// re-rendered from the parse.
	Raw [5]string
}

// certLimit is the game server's own cap (serverSide.cs:16), and the same
// reasoning applies: a certificate is about 1,200 characters, so anything near
// this is somebody probing rather than logging in.
const certLimit = 20000

// ErrBadCertificate is a refusal the player should see once, not a fault.
var ErrBadCertificate = fmt.Errorf("account certificate rejected")

// ParseCertificate splits and sanity-checks a certificate without verifying it.
//
// The checks mirror the ones the shipped server applies before it will touch a
// certificate (serverSide.cs:76-101) -- digits in the GUID, hex in the RSA
// fields -- because the fields end up in log lines, database keys and mail
// addressed by name, and a certificate is the one thing here that arrives
// entirely under the sender's control.
func ParseCertificate(raw string) (*Certificate, error) {
	if len(raw) > certLimit {
		return nil, fmt.Errorf("%w: too long", ErrBadCertificate)
	}

	// Trailing newline: t2csri_getAccountCertificate returns the stored line,
	// and the client sends it as it found it.
	fields := strings.Split(strings.TrimRight(raw, "\r\n"), "\t")
	if len(fields) != 5 {
		return nil, fmt.Errorf("%w: want 5 fields, got %d", ErrBadCertificate, len(fields))
	}

	name, guid := fields[0], fields[1]
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("%w: no name", ErrBadCertificate)
	}
	if guid == "" || !allDigits(guid) {
		return nil, fmt.Errorf("%w: guid is not a number", ErrBadCertificate)
	}

	key, err := ParsePublicKey(fields[2], fields[3])
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrBadCertificate, err)
	}
	if _, ok := parseHex(fields[4]); !ok {
		return nil, fmt.Errorf("%w: signature is not hex", ErrBadCertificate)
	}

	c := &Certificate{Name: name, GUID: guid, Key: key}
	copy(c.Raw[:], fields)
	return c, nil
}

// Verify checks the signature against the pinned TribesNext key.
//
// The scheme is the shipped client's, reproduced exactly: SHA-1 over the first
// four fields joined with tabs, compared against sig^E mod N. Both sides are
// compared as integers rather than as strings -- the client pads its hex to a
// fixed width before comparing (serverSide.cs:106-118) precisely because the
// modular exponentiation drops leading zeros, and comparing numbers sidesteps
// the whole question.
//
// What this proves: TribesNext issued this name and GUID for this key. What it
// does not prove: that whoever sent it holds the private half. A certificate is
// public -- the client broadcasts it to every game server it joins -- so
// possession has to be established separately, which is what the challenge in
// session.go is for.
func (c *Certificate) Verify() error {
	sig, ok := parseHex(c.Raw[4])
	if !ok {
		return fmt.Errorf("%w: signature is not hex", ErrBadCertificate)
	}

	signed := strings.Join([]string{c.Raw[0], c.Raw[1], c.Raw[2], c.Raw[3]}, "\t")
	sum := sha1.Sum([]byte(signed))
	want := new(big.Int).SetBytes(sum[:])

	if authKey.Apply(sig).Cmp(want) != 0 {
		return fmt.Errorf("%w: not signed by TribesNext", ErrBadCertificate)
	}
	return nil
}

// Identity is what a verified certificate says about its holder.
type Identity struct {
	GUID string
	// Name is authoritative because it is inside the signature: TribesNext
	// vouched for this warrior name belonging to this GUID.
	Name string
	// Created is the account's registration date in unix seconds, or 0 for
	// "unknown". A certificate does not carry one, so it is always 0 now --
	// the field stays because the store reads 0 as "date this account from
	// first sighting" and existing rows keep whatever they already have.
	Created int64
}

// Identity of a verified certificate.
func (c *Certificate) Identity() Identity {
	return Identity{GUID: c.GUID, Name: c.Name}
}

// Fingerprint identifies the account key, for logs and for spotting the one
// thing a certificate cannot: a GUID whose key has changed since last time.
func (c *Certificate) Fingerprint() string {
	sum := sha1.Sum([]byte(c.Raw[2] + "\t" + c.Raw[3]))
	return hex.EncodeToString(sum[:])
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
