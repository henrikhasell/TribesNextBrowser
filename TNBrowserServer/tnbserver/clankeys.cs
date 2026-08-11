// TNBrowserServer -- the public keys clan certificates are checked against
//
// This file is data, not logic. Each key the backend signs with gets two lines:
//
//     $TNBS::ClanKeyE[<id>] = "<exponent, hex>";
//     $TNBS::ClanKeyN[<id>] = "<modulus, hex>";
//
// The id is the number the backend stamps into field 0 of every certificate it
// issues, which is how a server picks the key to check one with.
//
// # Where the lines come from
//
// The backend prints them when it makes the key, and writes them beside it:
//
//     tnserver -genkey clan.pem        ->  clan.pem  and  clan.pem.pub.cs
//
// Then either bake that file into the package --
//
//     tools/build-vl2.sh --clan-key clan.pem.pub.cs --host https://...
//
// -- which replaces this file inside the archive, or paste the two lines into a
// loose autoexec.cs beside the .vl2, which the game execs last and which
// therefore wins over anything in here.
//
// # More than one key at a time is normal
//
// Keep the old key's lines when a new one is added. A certificate issued under
// the old id stays valid until it expires, and certificates are minted for
// minutes rather than days, so the overlap is short -- but during it, both are
// in circulation.
//
// A server that has never heard of the id on a certificate does not refuse the
// player: it shows them without a tag. That is what makes rotation something a
// backend can do on its own schedule, with no server operator having to act.
//
// # There is nothing secret here
//
// These are public keys. They verify a signature and cannot make one. Publish
// them, commit them, paste them into a forum post -- the private half never
// leaves the backend that issues certificates.

// No keys configured. Every clan certificate is refused as "unknown key" and
// players connect exactly as they would with no clan system at all.
