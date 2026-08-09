#!/usr/bin/env python3
"""Stand-in for the TribesNext community backend.

Serves the documented shapes of the robot session endpoint and the JSON browser
API so the mod's GUI, parser and API layers can be developed and tested without
touching the live server -- which is beta, rate-limits under repeated probing,
and would need a real account for every write.

Plain HTTP on purpose. The client verifies certificates against
curl-ca-bundle.crt, so a self-signed HTTPS mock would simply fail to connect;
TLS is exercised against the real backend instead.

Point the mod at it with, from the game console:

    $TNB::Host = "http://172.17.0.1:8099";

172.17.0.1 is the default Docker bridge gateway, i.e. the host as seen from
inside the container.

The RSA handshake is *not* reproduced: answering a challenge needs the account
private key, which only exists after a real password login. The mock instead
hands out a session immediately, so everything downstream of authentication can
be tested. Run with --require-handshake to make it insist on the full
nonce/response exchange when testing the session layer against a real account.

Usage:
    ./tools/mockserver.py [--port 8099] [--require-handshake] [--latency MS]
"""

import argparse
import json
import random
import re
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse, parse_qs

# --------------------------------------------------------------------------
# Fixture data
#
# GUID 4510186 / "orange01" matches the account installed on this machine, so
# the mod's "my own profile" path exercises the same identity it will use live.
# --------------------------------------------------------------------------

NOW = 1785000000

USERS = {
    "4510186": {
        "guid": "4510186",
        "name": "orange01",
        "tag": "[TC]",
        "append": "0",
        "creation": str(NOW - 400 * 86400),
        "website": "www.tribesnext.com",
        "info": "Testing the in-game browser.\n\nSecond paragraph with a "
                'quote: "shazbot!" and a <a:www.example.com>link</a>.',
        "online": "1",
        "memberships": [
            {"id": "7", "name": "Test Clan", "rank": "4", "title": "Leader",
             "tag": "[TC]", "append": "0"},
            {"id": "9", "name": "Casual Alliance", "rank": "1",
             "title": "Member", "tag": "-CA-", "append": "1"},
        ],
    },
    "4120041": {
        "guid": "4120041",
        "name": "Shifter",
        "tag": "[TC]",
        "append": "0",
        "creation": str(NOW - 900 * 86400),
        "website": "",
        "info": "Long-time defender. Ask me about mortar arcs.",
        "online": "0",
        "memberships": [
            {"id": "7", "name": "Test Clan", "rank": "2", "title": "Officer",
             "tag": "[TC]", "append": "0"},
        ],
    },
    "4200999": {
        "guid": "4200999",
        "name": "Ravage",
        "tag": "",
        "append": "0",
        "creation": str(NOW - 120 * 86400),
        "website": "example.org",
        "info": "",
        "online": "1",
        "memberships": [],
    },
    "4300777": {
        "guid": "4300777",
        "name": "orangeade",
        "tag": "-CA-",
        "append": "1",
        "creation": str(NOW - 30 * 86400),
        "website": "",
        "info": "Unicode check: café naïve — and a backslash \\ too.",
        "online": "0",
        "memberships": [
            {"id": "9", "name": "Casual Alliance", "rank": "0",
             "title": "Recruit", "tag": "-CA-", "append": "1"},
        ],
    },
}

CLANS = {
    "7": {
        "id": "7",
        "name": "Test Clan",
        "tag": "[TC]",
        "append": "0",
        "recruiting": "1",
        "website": "www.testclan.example",
        "info": "We are a test clan. Scrims Tuesdays.",
        "creation": str(NOW - 800 * 86400),
        "picture": "",
        "active": "1",
        "members": [
            {"guid": "4510186", "name": "orange01", "tag": "[TC]",
             "append": "0", "rank": "4", "title": "Leader", "online": "1"},
            {"guid": "4120041", "name": "Shifter", "tag": "[TC]",
             "append": "0", "rank": "2", "title": "Officer", "online": "0"},
        ],
    },
    "9": {
        "id": "9",
        "name": "Casual Alliance",
        "tag": "-CA-",
        "append": "1",
        "recruiting": "0",
        "website": "",
        "info": "",
        "creation": str(NOW - 200 * 86400),
        "picture": "",
        "active": "1",
        "members": [
            {"guid": "4510186", "name": "orange01", "tag": "[TC]",
             "append": "0", "rank": "1", "title": "Member", "online": "1"},
            {"guid": "4300777", "name": "orangeade", "tag": "-CA-",
             "append": "1", "rank": "0", "title": "Recruit", "online": "0"},
        ],
    },
}

# Invitations pending for a player, keyed by invitee GUID.
USER_INVITES = {
    "4510186": [
        {"sender": {"guid": "4120041", "name": "Shifter", "tag": "[TC]",
                    "append": "0"},
         "clan": {"id": "7", "name": "Test Clan", "tag": "[TC]", "append": "0"}},
    ],
}

# Invitations a clan has outstanding, keyed by clan id.
CLAN_INVITES = {
    "7": [
        {"guid": "4200999", "name": "Ravage", "tag": "", "append": "0"},
    ],
}

# Mail fixtures. The live server's item field names could not be observed (the
# inbox is empty and sending is disabled), so the mock uses the spellings
# TNBMailField tries first; the client accepts several alternatives.
MAIL = {
    "4510186": [
        {"id": "11", "from": "Shifter", "fromguid": "4120041",
         "subject": "Scrim on Tuesday?", "date": str(NOW - 3600),
         "body": "We are short a defender. Interested?", "unread": "1"},
        {"id": "12", "from": "Ravage", "fromguid": "4200999",
         "subject": "gg", "date": str(NOW - 86400),
         "body": "Good games last night.\n\n-- Ravage", "unread": "0"},
    ],
}

HISTORY = {
    "user": [
        {"time": str(NOW - 86400), "event": "Joined clan Test Clan"},
        {"time": str(NOW - 200000), "event": "Changed profile text"},
    ],
    "clan": [
        {"time": str(NOW - 86400), "event": "orange01 promoted Shifter"},
        {"time": str(NOW - 500000), "event": "Clan created"},
    ],
}

STATE_LOCK = threading.Lock()
SESSIONS = {}       # uuid -> guid
CHALLENGES = {}     # guid -> nonce


def ok():
    return {"status": "success"}


def err(msg):
    return {"status": "error", "msg": msg}


def clan_rank_of(clan_id, guid):
    for m in CLANS.get(clan_id, {}).get("members", []):
        if m["guid"] == guid:
            return int(m["rank"])
    return -1


# --------------------------------------------------------------------------
# Browser API methods
# --------------------------------------------------------------------------

def m_usersearch(guid, p):
    q = (p.get("q") or "").lower()
    if not q:
        return []
    return [{"guid": u["guid"], "name": u["name"], "tag": u["tag"],
             "append": u["append"]}
            for u in USERS.values() if u["name"].lower().startswith(q)]


def m_userview(guid, p):
    u = USERS.get(str(p.get("id", "")))
    return u if u else {}


def m_userhistory(guid, p):
    return HISTORY["user"]


def m_clansearch(guid, p):
    q = (p.get("q") or "").lower()
    if not q:
        return []
    return [{"id": c["id"], "name": c["name"]}
            for c in CLANS.values() if q in c["name"].lower()]


def m_clanview(guid, p):
    c = CLANS.get(str(p.get("id", "")))
    return c if c else {}


def m_clanhistory(guid, p):
    return HISTORY["clan"]


def m_userinvites(guid, p):
    return USER_INVITES.get(guid, [])


def m_clanviewinvites(guid, p):
    cid = str(p.get("id", ""))
    if clan_rank_of(cid, guid) < 2:
        return err("insufficient rank to view invitations")
    return {"status": "success", "payload": CLAN_INVITES.get(cid, [])}


def m_userinfo(guid, p):
    USERS[guid]["info"] = p.get("info", "")
    return ok()


def m_usersite(guid, p):
    USERS[guid]["site"] = p.get("site", "")
    USERS[guid]["website"] = p.get("site", "")
    return ok()


def m_userclan(guid, p):
    cid = str(p.get("id", ""))
    if cid == "-1":
        USERS[guid]["tag"] = ""
        USERS[guid]["append"] = "0"
        return ok()
    if clan_rank_of(cid, guid) < 0:
        return err("not a member of that clan")
    USERS[guid]["tag"] = CLANS[cid]["tag"]
    USERS[guid]["append"] = CLANS[cid]["append"]
    return ok()


def m_username(guid, p):
    # Disabled server-side during the beta; the real backend rejects it too.
    return err("name changes are disabled")


def m_useraccept(guid, p):
    cid = str(p.get("id", ""))
    pending = USER_INVITES.get(guid, [])
    match = [i for i in pending if i["clan"]["id"] == cid]
    if not match:
        return err("no such invitation")
    USER_INVITES[guid] = [i for i in pending if i["clan"]["id"] != cid]
    u = USERS[guid]
    CLANS[cid]["members"].append(
        {"guid": guid, "name": u["name"], "tag": u["tag"],
         "append": u["append"], "rank": "0", "title": "Recruit",
         "online": u["online"]})
    u["memberships"].append(
        {"id": cid, "name": CLANS[cid]["name"], "rank": "0",
         "title": "Recruit", "tag": CLANS[cid]["tag"],
         "append": CLANS[cid]["append"]})
    return ok()


def m_userreject(guid, p):
    cid = str(p.get("id", ""))
    USER_INVITES[guid] = [i for i in USER_INVITES.get(guid, [])
                          if i["clan"]["id"] != cid]
    return ok()


def m_userleave(guid, p):
    cid = str(p.get("id", ""))
    if clan_rank_of(cid, guid) < 0:
        return err("not a member of that clan")
    CLANS[cid]["members"] = [m for m in CLANS[cid]["members"]
                             if m["guid"] != guid]
    USERS[guid]["memberships"] = [m for m in USERS[guid]["memberships"]
                                  if m["id"] != cid]
    return ok()


def m_createclan(guid, p):
    tag, name = p.get("tag", ""), p.get("name", "")
    if not tag or not name:
        return err("tag and name are required")
    if any(c["name"].lower() == name.lower() for c in CLANS.values()):
        return err("a clan with that name already exists")
    cid = str(max(int(k) for k in CLANS) + 1)
    append = "1" if str(p.get("append", "")).lower() in ("1", "yes", "true") else "0"
    u = USERS[guid]
    CLANS[cid] = {
        "id": cid, "name": name, "tag": tag, "append": append,
        "recruiting": "0", "website": "", "info": "",
        "creation": str(NOW), "picture": "", "active": "1",
        "members": [{"guid": guid, "name": u["name"], "tag": u["tag"],
                     "append": u["append"], "rank": "4", "title": "Leader",
                     "online": u["online"]}],
    }
    u["memberships"].append({"id": cid, "name": name, "rank": "4",
                             "title": "Leader", "tag": tag, "append": append})
    return ok()


def _clan_setter(field, min_rank=3):
    def setter(guid, p):
        cid = str(p.get("id", ""))
        if cid not in CLANS:
            return err("no such clan")
        if clan_rank_of(cid, guid) < min_rank:
            return err("insufficient rank")
        CLANS[cid][field] = p.get("v", "")
        return ok()
    return setter


m_claninfo = _clan_setter("info", min_rank=2)
m_clanname = _clan_setter("name")
m_clansite = _clan_setter("website", min_rank=2)
m_clanpicture = _clan_setter("picture", min_rank=2)


def m_clanrecruit(guid, p):
    cid = str(p.get("id", ""))
    if clan_rank_of(cid, guid) < 2:
        return err("insufficient rank")
    v = str(p.get("v", "")).lower()
    CLANS[cid]["recruiting"] = "1" if v in ("1", "yes", "true") else "0"
    return ok()


def m_clantag(guid, p):
    cid = str(p.get("id", ""))
    if clan_rank_of(cid, guid) < 3:
        return err("insufficient rank")
    CLANS[cid]["tag"] = p.get("tag", "")
    CLANS[cid]["append"] = "1" if str(p.get("append", "")).lower() in (
        "1", "yes", "true") else "0"
    return ok()


def m_claninvite(guid, p):
    cid, to = str(p.get("id", "")), str(p.get("to", ""))
    if clan_rank_of(cid, guid) < 2:
        return err("insufficient rank")
    if to not in USERS:
        return err("no such player")
    if clan_rank_of(cid, to) >= 0:
        return err("player is already a member")
    CLAN_INVITES.setdefault(cid, []).append(
        {"guid": to, "name": USERS[to]["name"], "tag": USERS[to]["tag"],
         "append": USERS[to]["append"]})
    USER_INVITES.setdefault(to, []).append({
        "sender": {"guid": guid, "name": USERS[guid]["name"],
                   "tag": USERS[guid]["tag"], "append": USERS[guid]["append"]},
        "clan": {"id": cid, "name": CLANS[cid]["name"],
                 "tag": CLANS[cid]["tag"], "append": CLANS[cid]["append"]}})
    return ok()


def m_clanrank(guid, p):
    cid, to = str(p.get("id", "")), str(p.get("to", ""))
    try:
        rank = int(p.get("rank", ""))
    except ValueError:
        return err("rank must be an integer 0 to 4")
    if not 0 <= rank <= 4:
        return err("rank must be an integer 0 to 4")
    mine = clan_rank_of(cid, guid)
    if mine < 3:
        return err("insufficient rank")
    if rank > mine:
        return err("cannot promote above your own rank")
    for m in CLANS.get(cid, {}).get("members", []):
        if m["guid"] == to:
            m["rank"] = str(rank)
            m["title"] = p.get("title", m["title"])
            for ms in USERS.get(to, {}).get("memberships", []):
                if ms["id"] == cid:
                    ms["rank"] = str(rank)
                    ms["title"] = m["title"]
            return ok()
    return err("target is not a member")


def m_clankick(guid, p):
    cid, to = str(p.get("id", "")), str(p.get("to", ""))
    mine = clan_rank_of(cid, guid)
    if mine < 3:
        return err("insufficient rank")
    if clan_rank_of(cid, to) >= mine:
        return err("cannot kick a player of equal or higher rank")
    CLANS[cid]["members"] = [m for m in CLANS[cid]["members"]
                             if m["guid"] != to]
    USERS[to]["memberships"] = [m for m in USERS[to]["memberships"]
                                if m["id"] != cid]
    return ok()


def m_clandisband(guid, p):
    cid = str(p.get("id", ""))
    if clan_rank_of(cid, guid) < 4:
        return err("only the leader may authorise a disband")
    v = str(p.get("v", "")).lower()
    CLANS[cid]["active"] = "0" if v in ("1", "yes", "true") else "1"
    return ok()


METHODS = {
    "usersearch": m_usersearch, "userview": m_userview,
    "userhistory": m_userhistory, "username": m_username,
    "userclan": m_userclan, "usersite": m_usersite, "userinfo": m_userinfo,
    "userinvites": m_userinvites, "useraccept": m_useraccept,
    "userreject": m_userreject, "userleave": m_userleave,
    "createclan": m_createclan,
    "clansearch": m_clansearch, "clanview": m_clanview,
    "clanhistory": m_clanhistory, "clanrecruit": m_clanrecruit,
    "claninfo": m_claninfo, "clantag": m_clantag, "clansite": m_clansite,
    "clanname": m_clanname, "clanpicture": m_clanpicture,
    "claninvite": m_claninvite, "clanviewinvites": m_clanviewinvites,
    "clanrank": m_clanrank, "clankick": m_clankick,
    "clandisband": m_clandisband,
}


# --------------------------------------------------------------------------
# Mail API (json_mail.php)
#
# Mirrors what probing the live endpoint established: count returns the number
# as a JSON string, read with no payload returns the list, read with an id
# returns one message, delete takes an id, and send is refused.
# --------------------------------------------------------------------------

def mail_count(guid, p):
    return str(len(MAIL.get(guid, [])))


def mail_read(guid, p):
    box = MAIL.get(guid, [])
    if not p or "id" not in p:
        return box

    # Reading a message BY ID marks it read; listing a folder does not. That
    # asymmetry is the whole of the read model, on the real backend too
    # (store.MailRead -> UPDATE mail SET unread = FALSE), so the mock has to
    # reproduce it or a client that never marks anything read still passes.
    #
    # The reply carries the message as it was when opened, which is why the
    # copy is taken before the flag is cleared.
    hit = [m for m in box if m["id"] == str(p.get("id"))]
    out = [dict(m) for m in hit]
    for m in hit:
        m["unread"] = "0"
    return out


def mail_delete(guid, p):
    box = MAIL.get(guid, [])
    MAIL[guid] = [m for m in box if m["id"] != str(p.get("id"))]
    return []


def mail_send(guid, p):
    # The live server answers "500 Invalid Parameters" for every shape tried.
    # Reproduced so the client's failure path is exercised rather than guessed.
    return None


MAIL_METHODS = {
    "count": mail_count, "read": mail_read,
    "delete": mail_delete, "send": mail_send,
}


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    server_version = "MockTribesNext/1.0"

    def log_message(self, fmt, *args):
        print("  %s" % (fmt % args), flush=True)

    def _send(self, body, content_type="text/html"):
        if self.server.latency:
            time.sleep(self.server.latency / 1000.0)
        raw = body.encode("utf-8", "replace")
        self.send_response(200)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(raw)))
        self.send_header("Connection", "close")
        self.end_headers()
        self.wfile.write(raw)

    def _deny(self, code, text):
        raw = text.encode()
        self.send_response(code)
        self.send_header("Content-Type", "text/html")
        self.send_header("Content-Length", str(len(raw)))
        self.send_header("Connection", "close")
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):
        self._handle({})

    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(length).decode("utf-8", "replace")
        self._handle(parse_qs(raw))

    def _handle(self, posted):
        parsed = urlparse(self.path)
        params = parse_qs(parsed.query)
        params.update(posted)
        get = lambda k: (params.get(k) or [""])[0]

        if parsed.path.endswith("robot_login.php"):
            return self._login(get)
        if parsed.path.endswith("json_browser.php"):
            return self._browser(get)
        if parsed.path.endswith("json_mail.php"):
            return self._mail(get)
        return self._deny(404, "<h1>Not Found</h1>")

    # -- session -----------------------------------------------------------

    def _login(self, get):
        guid, nonce = get("guid"), get("nonce")
        uuid, response = get("uuid"), get("response")

        if not guid:
            return self._send("ERR: No GUID specified.")

        with STATE_LOCK:
            if uuid:
                if SESSIONS.get(uuid) == guid:
                    return self._send("REFRESHED")
                return self._send("TIMEOUT")

            if nonce:
                if not re.fullmatch(r"[0-9a-fA-F]+", nonce):
                    return self._send("ERR: Malformed nonce.")
                if not self.server.require_handshake:
                    # Skip straight to a session: answering a real challenge
                    # needs the account private key.
                    token = "mock-%08x" % random.getrandbits(32)
                    SESSIONS[token] = guid
                    return self._send("UUID: " + token)
                CHALLENGES[guid] = nonce
                blob = "%0256x" % random.getrandbits(512)
                return self._send("CHALLENGE: " + blob)

            if response:
                if guid not in CHALLENGES:
                    return self._send("ERR: No challenge outstanding.")
                del CHALLENGES[guid]
                token = "mock-%08x" % random.getrandbits(32)
                SESSIONS[token] = guid
                return self._send("UUID: " + token)

        return self._send("ERR: Nothing to do.")

    # -- browser -----------------------------------------------------------

    def _browser(self, get):
        guid, uuid, method = get("guid"), get("uuid"), get("method")

        if not guid or not uuid:
            return self._deny(401, "<h1>Fatal Error</h1><h2>401 "
                                   "Authentication Required</h2>")
        with STATE_LOCK:
            authorised = SESSIONS.get(uuid) == guid or self.server.open_auth
        if not authorised:
            return self._deny(401, "<h1>Fatal Error</h1><h2>401 "
                                   "Authentication Required</h2>")

        fn = METHODS.get(method)
        if fn is None:
            return self._deny(501, "<h1>Fatal Error</h1><h2>501 Not "
                                   "Implemented</h2>")

        payload = {}
        raw = get("payload")
        if raw:
            try:
                payload = json.loads(raw)
            except ValueError:
                return self._send(json.dumps(err("malformed payload")))

        if guid not in USERS:
            return self._send(json.dumps(err("unknown account")))

        with STATE_LOCK:
            result = fn(guid, payload)
        return self._send(json.dumps(result))

    def _mail(self, get):
        guid, uuid, method = get("guid"), get("uuid"), get("method")

        if not guid or not uuid:
            return self._deny(401, "<h1>Fatal Error</h1><h2>401 "
                                   "Authentication Required</h2>")
        with STATE_LOCK:
            authorised = SESSIONS.get(uuid) == guid or self.server.open_auth
        if not authorised:
            return self._deny(401, "<h1>Fatal Error</h1><h2>401 "
                                   "Authentication Required</h2>")

        fn = MAIL_METHODS.get(method)
        if fn is None:
            return self._deny(501, "<h1>Fatal Error</h1><h2>501 Not "
                                   "Implemented</h2>")

        payload = {}
        raw = get("payload")
        if raw:
            try:
                payload = json.loads(raw)
            except ValueError:
                return self._deny(500, "<h1>Fatal Error</h1>"
                                       "<h2>500 Invalid Parameters</h2>")

        with STATE_LOCK:
            result = fn(guid, payload)

        if result is None:
            return self._deny(500, "<h1>Fatal Error</h1>"
                                   "<h2>500 Invalid Parameters</h2>")
        return self._send(json.dumps(result))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, default=8099)
    ap.add_argument("--require-handshake", action="store_true",
                    help="demand the full RSA nonce/response exchange")
    ap.add_argument("--open-auth", action="store_true",
                    help="accept any guid/uuid pair, for GUI work")
    ap.add_argument("--latency", type=int, default=0,
                    help="artificial delay per response, in milliseconds")
    args = ap.parse_args()

    srv = ThreadingHTTPServer(("0.0.0.0", args.port), Handler)
    srv.require_handshake = args.require_handshake
    srv.open_auth = args.open_auth
    srv.latency = args.latency
    print("Mock TribesNext backend on port %d "
          "(handshake=%s, open-auth=%s, latency=%dms)"
          % (args.port, args.require_handshake, args.open_auth, args.latency),
          flush=True)
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
