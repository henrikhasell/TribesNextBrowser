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

// tnb.k8s.henrik.si, issued 2026-08-11. The backend signs under this id; see
// TNB_CLAN_KEY_ID in .do/app.yaml.
$TNBS::ClanKeyE[1] = "10001";
$TNBS::ClanKeyN[1] = "b0fa629aaedbf728dda1565a5c8754319d1e0beee4dd9b9bf2c87500f6ea53f690f3f31185496a617fe29736a63a1f5b40990793ce7492aaaee50f0cf1171a12d84ed64dc194697f32d5b330dfe90b44e78fd44b651b5be44200d6982bef6faaf6df39e3d65d186efa7199ce674eb62c0b9a5ccd521a6d6c1b6a7894e4551658a49adb06c9680136b905cb3ddcf08054ef40da1e2f1276f891408ea244b3a83647df5d9b47fd8a420f8a739e868e9f3b0b967f62828f6f0f7646e8aba66ad3e47024375fe87f8d7941636fa162239ea6d54f1388e004ddfc6a77c587282e74c4ddf4b8e548abe311cbde21b71e4f8bbd01d51470c86c508465b438e777adace1";
