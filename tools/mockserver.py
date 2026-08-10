#!/usr/bin/env python3
"""Stand-in for the TNBrowser community backend.

Serves the robot session endpoint, the database proxy (`/db`) and the identity
certificate (`/cert`), so the mod's shim, parser and session layers can be
developed and tested without touching a real backend or a real database.

It is deliberately a second implementation of the same row schemas the Go
server produces, rather than a thin fake. The two are driven by the same test
suites inside the real game, so a difference between them is a real behavioural
difference -- which is the whole point of having both. Where this file and
`server/internal/dbproxy` disagree about a field index, one of them is wrong
about what the shipped client reads.

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
from datetime import datetime, timezone
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
        "creation": NOW - 400 * 86400,
        "website": "www.tribesnext.com",
        "graphic": "texticons/twb/twb_Missilelauncher.jpg",
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
        "creation": NOW - 900 * 86400,
        "website": "",
        "graphic": "",
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
        "creation": NOW - 120 * 86400,
        "website": "example.org",
        "graphic": "",
        "info": "",
        "online": "1",
        "memberships": [],
    },
    "4300777": {
        "guid": "4300777",
        "name": "orangeade",
        "tag": "-CA-",
        "append": "1",
        "creation": NOW - 30 * 86400,
        "website": "",
        "graphic": "",
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
        "creation": NOW - 800 * 86400,
        "picture": "",
        "members": [
            {"guid": "4510186", "rank": "4", "title": "Leader",
             "joined": NOW - 800 * 86400},
            {"guid": "4120041", "rank": "2", "title": "Officer",
             "joined": NOW - 700 * 86400},
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
        "creation": NOW - 200 * 86400,
        "picture": "",
        "members": [
            {"guid": "4510186", "rank": "1", "title": "Member",
             "joined": NOW - 200 * 86400},
            {"guid": "4300777", "rank": "0", "title": "Recruit",
             "joined": NOW - 30 * 86400},
        ],
    },
}

# Outstanding invitations and join requests, keyed by clan id. "kind" is what
# tells an invitation the tribe sent from a request a warrior made -- the wire
# cannot distinguish them, only who is asking can.
CLAN_INVITES = {
    "7": [
        {"id": "31", "guid": "4200999", "from": "4120041",
         "created": NOW - 2 * 86400, "kind": "invite"},
    ],
}

BUDDIES = {
    "4510186": [
        {"guid": "4120041", "since": NOW - 300 * 86400},
        {"guid": "4300777", "since": NOW - 20 * 86400},
    ],
}

BLOCKS = {
    "4510186": [
        {"guid": "4200999", "hits": "3"},
    ],
}

# Mail. The row schema is the client's: field 12 is what makes an unread row
# render bold, and the body is a line count followed by that many fields.
MAIL = {
    "4510186": [
        {"id": 11, "from": "4120041", "to": "4510186",
         "subject": "Scrim on Tuesday?", "created": NOW - 3600,
         "body": "We are short a defender. Interested?",
         "read": False, "folder": "inbox", "cc": False,
         "tolist": "orange01", "cclist": ""},
        {"id": 12, "from": "4200999", "to": "4510186",
         "subject": "gg", "created": NOW - 86400,
         "body": "Good games last night.\n-- Ravage",
         "read": True, "folder": "inbox", "cc": False,
         "tolist": "orange01", "cclist": ""},
        {"id": 9, "from": "4120041", "to": "4510186",
         "subject": "Old news", "created": NOW - 20 * 86400,
         "body": "Already thrown away.",
         "read": True, "folder": "deleted", "cc": False,
         "tolist": "orange01", "cclist": ""},
    ],
}

HISTORY = {
    "4510186": [
        {"time": NOW - 86400, "event": "Joined tribe Test Clan"},
        {"time": NOW - 200000, "event": "Changed profile text"},
    ],
}

NEWS = [
    {"id": 501, "category": 105, "headline": "Local stand-in serves news",
     "body": "Nothing in a retail install can render this pane.",
     "author": "4510186", "created": NOW - 4 * 86400, "updated": NOW - 4 * 86400},
]

FORUMS = [{"id": 1, "name": "General Discussion", "flag": 0, "security": 0}]

TOPICS = [
    {"id": 71, "forum": 1, "subject": "Welcome", "author": "4510186",
     "created": NOW - 10 * 86400, "updated": NOW - 9 * 86400},
]

POSTS = [
    {"id": 900, "topic": 71, "parent": 0, "author": "4510186",
     "subject": "Welcome", "body": "First post.", "created": NOW - 10 * 86400,
     "deleted": False},
]

# weblinksmenu::defaultList (weblinks.cs:1-56) is the client's own fallback and
# the only surviving record of what this pane served. Three of it is enough to
# prove the row shape.
WEB_LINKS = [
    {"name": "PlanetTribes", "address": "www.planettribes.com"},
    {"name": "Tribal War", "address": "www.tribalwar.com"},
    {"name": "5 Assed Monkey", "address": "www.5assedmonkey.com"},
]

# What the shipped dialogs set their own preview controls to
# (WarriorPropertiesDlg.gui:348, TribePropertiesDlg.gui:618). An empty graphic
# renders a permanently blank picture, because $PlayerGfx and $TribeGfx are the
# fallbacks the client uses and no shipped script assigns either.
DEFAULT_PLAYER_GFX = "texticons/twb/twb_Missilelauncher.jpg"
DEFAULT_TRIBE_GFX = "texticons/twb/twb_Laserrifle.jpg"


def graphic_or(path, fallback):
    """A graphic beginning with a digit would be read as a tribe count by the
    other consumer of ordinal 23 field 9."""
    return fallback if (not path or path[0].isdigit()) else path

STATE_LOCK = threading.Lock()
SESSIONS = {}       # uuid -> guid
CHALLENGES = {}     # guid -> nonce


# --------------------------------------------------------------------------
# Row helpers -- the same conventions as server/internal/dbproxy/dispatch.go
# --------------------------------------------------------------------------

def tab(*parts):
    return "\t".join("" if p is None else str(p) for p in parts)


def flag(b):
    return "1" if b else "0"


def date(unix):
    if not unix:
        return ""
    return datetime.fromtimestamp(int(unix), timezone.utc).strftime("%Y-%m-%d")


def body_lines(text):
    """An empty body is ONE empty line, not zero.

    Every row schema that carries a body ends "line count then that many
    lines", and a zero-line body makes every field after it land one place
    early -- which renders as a plausible pane with the wrong data in it.
    """
    return (text or "").replace("\r\n", "\n").split("\n")


def with_body(head, text):
    lines = body_lines(text)
    return tab(head, len(lines), *lines)


def ml_link(label, verb, *args):
    """A working <a:...> link in a mail body.

    The separator is a newline here and a TAB by the time the client sees it:
    the body is split on newlines into row fields, rejoined TAB-separated by
    getFields(%row,17) (webemail.cs:1147), and printed verbatim by EmailGetBody.
    GuiMLTextCtrl::onURL then splits the URL on TAB. For tribe invitations this
    is the only channel there is.
    """
    return "<a:" + "\n".join((verb,) + args) + ">" + label + "</a>"


def ok_status(*extra):
    return tab("0", "OK", *extra)


def ok_rows(rows):
    return {"status": ok_status(), "result": str(len(rows)), "rows": rows}


def ok_result(result):
    return {"status": ok_status(), "result": result, "rows": []}


def ok_with(status, result):
    return {"status": status, "result": result, "rows": []}


def fail(message):
    return {"status": tab("1", message), "result": "0", "rows": []}


def fields(args):
    return args.split("\t") if args else []


def field(args, n):
    f = fields(args)
    return f[n] if 0 <= n < len(f) else ""


def num(s):
    try:
        return int(str(s).strip())
    except (TypeError, ValueError):
        return 0


# --------------------------------------------------------------------------
# Fixture lookups
# --------------------------------------------------------------------------

def user_by_name(name):
    name = (name or "").strip().lower()
    for u in USERS.values():
        if u["name"].lower() == name:
            return u
    # getLinkName may have decorated the name with a tribe tag on either side.
    for c in CLANS.values():
        tag_ = c["tag"].lower()
        if not tag_:
            continue
        bare = None
        if name.startswith(tag_):
            bare = name[len(tag_):]
        elif name.endswith(tag_):
            bare = name[:-len(tag_)]
        if bare:
            for u in USERS.values():
                if u["name"].lower() == bare:
                    return u
    return None


def clan_by_name(name):
    name = (name or "").strip()
    if name in CLANS:
        return CLANS[name]
    for c in CLANS.values():
        if c["name"].lower() == name.lower():
            return c
    return None


def quad(guid):
    u = USERS.get(guid)
    if u is None:
        return ("(unknown)", "", "1", guid)
    return (u["name"], u["tag"], u["append"], guid)


def rank_in(clan, guid):
    for m in clan["members"]:
        if m["guid"] == guid:
            return num(m["rank"])
    return -1


# --------------------------------------------------------------------------
# The ordinals
# --------------------------------------------------------------------------

ORDINALS = {}


def on(form, ordinal):
    def register(fn):
        ORDINALS[(form, str(ordinal))] = fn
        return fn
    return register


# -- browser: profiles ------------------------------------------------------

@on("scalar", 22)
def get_tribe_profile(guid, args):
    c = clan_by_name(field(args, 0))
    if c is None:
        return fail("There is no tribe by that name.")
    status = ok_status(c["id"], c["name"], c["tag"], c["append"],
                       c["recruiting"], graphic_or(c["picture"], DEFAULT_TRIBE_GFX))
    return ok_with(status, c["info"])


@on("scalar", 23)
def get_warrior_profile(guid, args):
    u = user_by_name(field(args, 0))
    if u is None:
        return fail("There is no warrior by that name.")
    gfx = graphic_or(u["graphic"], DEFAULT_PLAYER_GFX)
    status = ok_status(u["name"], u["tag"], u["append"], u["guid"],
                       date(u["creation"]), u["online"], u["website"], gfx)
    return ok_with(status, u["info"])


# -- browser: lists ---------------------------------------------------------

def search_args(args):
    count = num(field(args, 2))
    if count <= 0 or count > 200:
        count = 100
    return field(args, 0), num(field(args, 1)), count


@on("array", 3)
def search_warriors(guid, args):
    q, start, count = search_args(args)
    hits = [u for u in USERS.values() if q.lower() in u["name"].lower()]
    hits.sort(key=lambda u: u["name"])
    return ok_rows([tab(u["guid"], u["name"], u["tag"], u["append"])
                    for u in hits[start:start + count]])


@on("array", 4)
def search_tribes(guid, args):
    q, start, count = search_args(args)
    hits = [c for c in CLANS.values() if q.lower() in c["name"].lower()]
    hits.sort(key=lambda c: c["name"])
    return ok_rows([tab(c["id"], c["name"], c["tag"])
                    for c in hits[start:start + count]])


@on("array", 5)
def get_buddy_list(guid, args):
    who = guid
    if field(args, 0):
        u = user_by_name(field(args, 0))
        if u is None:
            return fail("There is no warrior by that name.")
        who = u["guid"]
    rows = []
    for b in BUDDIES.get(who, []):
        n, t, a, g = quad(b["guid"])
        rows.append(tab(n, t, a, g, date(b["since"]),
                        USERS.get(b["guid"], {}).get("online", "0")))
    return ok_rows(rows)


@on("array", 6)
def get_tribe_members(guid, args):
    c = clan_by_name(field(args, 0))
    if c is None:
        return fail("There is no tribe by that name.")
    can_admin = rank_in(c, guid) >= 2
    rows = []
    for m in c["members"]:
        n, t, a, g = quad(m["guid"])
        rows.append(tab(n, t, a, g, m["title"], m["rank"], date(m["joined"]),
                        "", flag(can_admin and g != guid),
                        USERS.get(g, {}).get("online", "0")))
    return ok_rows(rows)


@on("array", 10)
def get_tribe_news(guid, args):
    # Cut before release: the pane sets state = "done" unconditionally and
    # discards whatever arrives (webbrowser.cs:1410).
    if clan_by_name(field(args, 0)) is None:
        return fail("There is no tribe by that name.")
    return ok_rows([])


@on("array", 11)
def get_tribe_invites(guid, args):
    c = clan_by_name(field(args, 0))
    if c is None:
        return fail("There is no tribe by that name.")
    rows = []
    for inv in CLAN_INVITES.get(c["id"], []):
        rows.append(tab(inv["id"], date(inv["created"]),
                        *quad(inv["from"]), *quad(inv["guid"]),
                        flag(inv["from"] == guid),
                        USERS.get(inv["guid"], {}).get("online", "0")))
    return ok_rows(rows)


@on("array", 12)
def get_warrior_history(guid, args):
    u = user_by_name(field(args, 0))
    if u is None:
        return fail("There is no warrior by that name.")
    # The only ordinal whose rows are not field-structured: each is one line of
    # display text, appended verbatim to a GuiMLTextCtrl.
    return ok_rows([date(h["time"]) + "  " + h["event"]
                    for h in HISTORY.get(u["guid"], [])])


@on("array", 13)
def get_warrior_tribe_list(guid, args):
    u = user_by_name(field(args, 0))
    if u is None:
        return fail("There is no warrior by that name.")
    rows = []
    for m in u["memberships"]:
        rows.append(tab(m["name"], "", m["id"], m["rank"],
                        flag(num(m["rank"]) >= 2), m["title"]))
    return ok_rows(rows)


# -- browser: writes --------------------------------------------------------

# -- notifications ----------------------------------------------------------
#
# Tribe membership changes hands through panes that show no history, so the
# party who did not perform the change has no way to learn of it: a kicked
# warrior sees a tab quietly missing, a tribe never hears that somebody left,
# and an answered invitation simply disappears from the list that held it. Mail
# is the only channel the client already checks by itself, and the shipped
# scripts already use it for the invitation these answer.
#
# Mirrored from the Go backend rather than from TribesNext's PHP, which sent
# none of these -- the one place this fixture deliberately models the new
# backend, so the client suites can assert the same behaviour against both.

def _notify(to_guid, from_guid, subject, body):
    if to_guid and to_guid != from_guid and to_guid in USERS:
        _deliver(to_guid, from_guid, subject, body)


def _notify_all(to_guids, from_guid, subject, body):
    for g in to_guids:
        _notify(g, from_guid, subject, body)


def _members_except(clan, guid):
    return [m["guid"] for m in clan["members"] if m["guid"] != guid]


def _admins_except(clan, guid):
    return [m["guid"] for m in clan["members"]
            if m["guid"] != guid and num(m["rank"]) >= 2]


def _clan_write(guid, args, message, min_rank=2):
    c = clan_by_name(field(args, 0))
    if c is None:
        return fail("There is no tribe by that name.")
    if rank_in(c, guid) < min_rank:
        return fail("You do not have the rank to do that.")
    return ok_result(message)


@on("scalar", 15)
def set_tribe_description(guid, args):
    c = clan_by_name(field(args, 0))
    if c is None:
        return fail("There is no tribe by that name.")
    if rank_in(c, guid) < 2:
        return fail("You do not have the rank to do that.")
    c["info"] = "\n".join(fields(args)[2:])
    return ok_result("The tribe description has been updated.")


@on("scalar", 16)
def create_tribe(guid, args):
    """The dialog sends six fields, and the last two are the description.

    This used to answer ok and store nothing, which made it useless as a
    fixture for the one thing the ordinal is easy to get wrong: the Go backend
    read three of the six fields and dropped the description, and a mock that
    keeps no clan cannot tell anyone. It now founds the clan, so a test can ask
    for the profile back.
    """
    name = field(args, 0)
    if not name:
        return fail("A tribe needs a name.")
    if clan_by_name(name) is not None:
        return fail("There is already a tribe by that name.")

    new_id = str(max([num(i) for i in CLANS] + [100]) + 1)
    CLANS[new_id] = {
        "id": new_id,
        "name": name,
        "tag": field(args, 1),
        "append": flag(field(args, 2) in ("1", "yes", "true")),
        "recruiting": flag(field(args, 3) in ("1", "yes", "true")),
        "website": "",
        # Field 4 is the client's own line count; the description is the tail,
        # exactly as ordinal 15 sends it.
        "info": "\n".join(fields(args)[5:]),
        "creation": NOW,
        "picture": "",
        "members": [{"guid": guid, "rank": "4", "title": "Leader",
                     "joined": NOW}],
    }
    # The founder's membership as well, because the client re-reads its
    # certificate straight after this (WonUpdateCertificate, webbrowser.cs:774)
    # and builds the tribe's tab from what comes back.
    USERS[guid]["memberships"].append(
        {"id": new_id, "name": name, "rank": "4", "title": "Leader",
         "tag": CLANS[new_id]["tag"], "append": CLANS[new_id]["append"]})
    return ok_result(name)


@on("scalar", 17)
def set_warrior_description(guid, args):
    USERS[guid]["info"] = "" if args == "NONE" else args
    return ok_result("Your description has been updated.")


@on("scalar", 18)
def delete_tribe(guid, args):
    c = clan_by_name(field(args, 0))
    if c is None:
        return fail("There is no tribe by that name.")
    if rank_in(c, guid) < 4:
        return fail("You do not have the rank to do that.")

    # Disbanding is an authorisation, not a delete: every leader has to record
    # one. This fixture keeps no votes, so a clan with a single leader -- which
    # every fixture clan has -- disbands on the first, matching what the backend
    # does in that case.
    _notify_all(_members_except(c, guid), guid,
                "Tribe disbanded: " + c["name"],
                USERS[guid]["name"] + " has disbanded " + c["name"] +
                ". You are no longer a member of it.")
    return ok_result("Your disband authorisation has been recorded.")


@on("scalar", 19)
def kick_member(guid, args):
    c = clan_by_name(field(args, 1))
    if c is None:
        return fail("There is no tribe by that name.")
    if rank_in(c, guid) < 2:
        return fail("You do not have the rank to do that.")

    u = user_by_name(field(args, 0))
    if u is not None:
        _notify(u["guid"], guid, "Removed from " + c["name"],
                USERS[guid]["name"] + " has removed you from " + c["name"] + ".")
    return ok_result("That warrior has been removed from the tribe.")


@on("scalar", 20)
def toggle_tribe_flag(guid, args):
    what = field(args, 0).lower()
    c = clan_by_name(field(args, 1))
    if c is None:
        return fail("There is no tribe by that name.")
    if what == "recruiting":
        c["recruiting"] = flag(field(args, 2) in ("1", "yes", "true"))
    elif what == "appending":
        c["append"] = flag(field(args, 2) in ("1", "yes", "true"))
    else:
        return fail('"%s" is not a tribe flag this server knows.' % what)
    return ok_result("Done.")


@on("scalar", 21)
def set_member_profile(guid, args):
    c = clan_by_name(field(args, 0))
    if c is None:
        return fail("There is no tribe by that name.")
    if rank_in(c, guid) < 3:
        return fail("You do not have the rank to do that.")

    u = user_by_name(field(args, 1))
    if u is None:
        return fail("There is no warrior by that name.")
    title, rank = field(args, 2), num(field(args, 3))

    for m in c["members"]:
        if m["guid"] != u["guid"]:
            continue
        before = num(m["rank"])
        m["rank"], m["title"] = str(rank), title
        # Not for a title edit alone: the same dialog sends this ordinal for
        # one, and mail nobody needs buries the mail they do.
        if rank != before:
            verb = "promoted" if rank > before else "demoted"
            _notify(u["guid"], guid, "Rank changed in " + c["name"],
                    "%s has %s you to %s in %s."
                    % (USERS[guid]["name"], verb, title, c["name"]))
        break

    return ok_result("That member's profile has been updated.")


@on("scalar", 24)
def leave_tribe(guid, args):
    c = clan_by_name(field(args, 0))
    if c is None:
        return fail("There is no tribe by that name.")

    _notify_all(_admins_except(c, guid), guid, "Member left " + c["name"],
                USERS[guid]["name"] + " has left " + c["name"] + ".")
    return ok_result("You have left the tribe.")


@on("scalar", 25)
def set_primary_tribe(guid, args):
    arg = field(args, 0)
    if arg in ("", "0", "-1"):
        return ok_result("")
    c = clan_by_name(arg)
    if c is None:
        return fail("There is no tribe by that name.")
    return ok_result(c["name"])


@on("scalar", 26)
def clear_buddy(guid, args):
    BUDDIES[guid] = []
    return ok_result("Your buddy list has been cleared.")


@on("scalar", 27)
def invite_to_tribe(guid, args):
    c = clan_by_name(field(args, 0))
    if c is None:
        return fail("There is no tribe by that name.")
    u = user_by_name(field(args, 1))
    if u is None:
        return fail("There is no warrior by that name.")
    if rank_in(c, guid) < 2:
        return fail("You do not have the rank to do that.")

    # No client query lists a player's own invitations, so the invitation is
    # mailed with links that answer it.
    body = "\n".join([
        USERS[guid]["name"] + " has invited you to join " + c["name"] + ".",
        "",
        ml_link("Accept", "acceptinvite", c["name"], u["name"]) + "    " +
        ml_link("Reject", "rejectinvite", c["name"], u["name"]),
    ])
    _deliver(u["guid"], guid, "Tribe invitation: " + c["name"], body)
    return ok_result(c["name"])


@on("scalar", 28)
def answer_invitation(guid, args):
    verb = field(args, 0).lower()
    c = clan_by_name(field(args, 1))
    if c is None:
        return fail("There is no tribe by that name.")
    if verb not in ("accept", "reject", "cancel"):
        return fail('"%s" is not something that can be done with an '
                    'invitation.' % verb)

    # Field 2 names somebody else only when an admin is answering a request the
    # warrior made; otherwise it is a warrior answering their own invitation.
    subject = guid
    named = user_by_name(field(args, 2))
    if named is not None and rank_in(c, guid) >= 2:
        subject = named["guid"]

    box = CLAN_INVITES.get(c["id"], [])
    inv = next((i for i in box if i["guid"] == subject), None)
    if inv is None:
        return ok_result(c["name"])
    box.remove(inv)

    if verb == "cancel":
        return ok_result(c["name"])

    # Whoever raised it hears how it ended. Nothing else would tell them: no
    # client query lists answered invitations, so from the other side an
    # acceptance and a rejection look identical.
    request = inv["kind"] == "request"
    tell = subject if request else inv["from"]
    what = "request to join" if request else "invitation to join"
    outcome = "accepted" if verb == "accept" else "declined"
    heading = "Join request" if request else "Invitation"
    _notify(tell, guid, "%s %s: %s" % (heading, outcome, c["name"]),
            "%s has %s your %s %s."
            % (USERS[guid]["name"], outcome, what, c["name"]))

    return ok_result(c["name"])


@on("scalar", 29)
def set_tribe_graphic(guid, args):
    c = clan_by_name(field(args, 0))
    if c is None:
        return fail("There is no tribe by that name.")
    if rank_in(c, guid) < 2:
        return fail("You do not have the rank to do that.")
    c["picture"] = field(args, 1)
    return ok_result("The tribe graphic has been updated.")


@on("scalar", 30)
def set_tribe_tag(guid, args):
    c = clan_by_name(field(args, 0))
    if c is None:
        return fail("There is no tribe by that name.")
    if rank_in(c, guid) < 3:
        return fail("You do not have the rank to do that.")
    c["tag"] = field(args, 1)
    return ok_result("The tribe tag has been updated.")


@on("scalar", 31)
def set_player_graphic(guid, args):
    USERS[guid]["graphic"] = field(args, 0)
    return ok_result("Your graphic has been updated.")


@on("scalar", 32)
def set_player_url(guid, args):
    USERS[guid]["website"] = field(args, 0)
    return ok_result("Your web address has been updated.")


@on("scalar", 33)
def set_player_name(guid, args):
    return fail("Your warrior name belongs to your TribesNext account and "
                "must be changed there.")


@on("scalar", 34)
def request_invite(guid, args):
    c = clan_by_name(field(args, 0))
    if c is None:
        return fail("There is no tribe by that name.")
    if c["recruiting"] != "1":
        return fail("That tribe is not recruiting.")
    if rank_in(c, guid) >= 0:
        return fail("You are already a member of that tribe.")

    body = "\n".join([
        USERS[guid]["name"] + " has asked to join " + c["name"] + ".",
        "",
        ml_link("Accept", "acceptinvite", c["name"], USERS[guid]["name"]) +
        "    " +
        ml_link("Reject", "rejectinvite", c["name"], USERS[guid]["name"]),
    ])
    for m in c["members"]:
        if num(m["rank"]) >= 2:
            _deliver(m["guid"], guid, "Join request: " + c["name"], body)

    # Status field 1 goes straight into a MessageBoxOK (webbrowser.cs:1446).
    return {"status": ok_status("Your request has been sent to the tribe's "
                                "administrators."),
            "result": c["name"], "rows": []}


@on("scalar", 63)
def post_admin_action(guid, args):
    # WON kept its staff in tribe 1401; no fixture account is in it.
    return fail("You do not have moderator privileges.")


# -- email ------------------------------------------------------------------

def mail_row(m):
    head = tab(m["id"], *quad(m["from"]), *quad(m["to"]), date(m["created"]),
               flag(m["cc"]), flag(m["folder"] == "deleted"), flag(m["read"]),
               m["tolist"], m["cclist"], m["subject"])
    return with_body(head, m["body"])


@on("array", 1)
def get_mail(guid, args):
    # The argument is a high-water mark and filtering on it is not optional:
    # ignoring it makes the inbox grow without bound across polls.
    since = num(field(args, 0))
    rows = [mail_row(m) for m in MAIL.get(guid, [])
            if m["folder"] == "inbox" and m["id"] > since]
    return ok_rows(rows)


@on("array", 14)
def get_deleted_mail(guid, args):
    rows = [mail_row(m) for m in MAIL.get(guid, [])
            if m["folder"] == "deleted"]
    if not rows:
        return {"status": ok_status("Your deleted folder is empty."),
                "result": "0", "rows": []}
    return ok_rows(rows)


@on("array", 2)
def get_block_list(guid, args):
    rows = []
    for b in BLOCKS.get(guid, []):
        rows.append(tab(*quad(b["guid"]), b["hits"]))
    return ok_rows(rows)


def _deliver(to_guid, from_guid, subject, body):
    box = MAIL.setdefault(to_guid, [])
    next_id = max([m["id"] for m in box] + [100]) + 1
    box.append({"id": next_id, "from": from_guid, "to": to_guid,
                "subject": subject, "created": NOW, "body": body,
                "read": False, "folder": "inbox", "cc": False,
                "tolist": USERS.get(to_guid, {}).get("name", ""), "cclist": ""})


@on("scalar", 5)
def send_mail(guid, args):
    to = field(args, 0)
    if not to:
        return fail("No recipient.")
    u = user_by_name(to)
    if u is None:
        return fail("There is no warrior by that name.")
    _deliver(u["guid"], guid, field(args, 2), "\n".join(fields(args)[3:]))
    return ok_result("Your message has been sent.")


@on("scalar", 6)
def delete_mail(guid, args):
    mid = num(field(args, 0))
    for m in MAIL.get(guid, []):
        if m["id"] == mid:
            m["folder"] = "deleted"
            return ok_result("Message deleted.")
    return fail("No such message.")


@on("scalar", 35)
def remove_mail_permanently(guid, args):
    mid = num(field(args, 0))
    MAIL[guid] = [m for m in MAIL.get(guid, []) if m["id"] != mid]
    return ok_result("Message removed.")


@on("scalar", 7)
def mark_mail_read(guid, args):
    # Fire-and-forget: the only call site that passes neither a proxy object
    # nor a key, so this answer is reassembled and thrown away.
    mid = num(field(args, 0))
    for m in MAIL.get(guid, []):
        if m["id"] == mid:
            m["read"] = True
    return ok_result("1")


@on("scalar", 9)
def add_block(guid, args):
    u = user_by_name(field(args, 0))
    if u is None:
        return fail("There is no warrior by that name.")
    box = BLOCKS.setdefault(guid, [])
    if not any(b["guid"] == u["guid"] for b in box):
        box.append({"guid": u["guid"], "hits": "0"})
    return {"status": ok_status("Mail from that warrior will no longer "
                                "reach you."),
            "result": "1", "rows": []}


@on("scalar", 8)
def remove_block(guid, args):
    u = user_by_name(field(args, 0))
    if u is None:
        return fail("There is no warrior by that name.")
    BLOCKS[guid] = [b for b in BLOCKS.get(guid, []) if b["guid"] != u["guid"]]
    return {"status": ok_status("That warrior is no longer blocked."),
            "result": "1", "rows": []}


@on("scalar", 10)
def add_buddy(guid, args):
    u = user_by_name(field(args, 0))
    if u is None:
        return fail("There is no warrior by that name.")
    box = BUDDIES.setdefault(guid, [])
    if not any(b["guid"] == u["guid"] for b in box):
        box.append({"guid": u["guid"], "since": NOW})
    return {"status": ok_status("Added to your buddy list."),
            "result": "1", "rows": []}


@on("scalar", 11)
def drop_buddy(guid, args):
    u = user_by_name(field(args, 0))
    if u is None:
        return fail("There is no warrior by that name.")
    BUDDIES[guid] = [b for b in BUDDIES.get(guid, []) if b["guid"] != u["guid"]]
    return {"status": ok_status("Removed from your buddy list."),
            "result": "1", "rows": []}


@on("scalar", 69)
def get_online_status(guid, args):
    # A fixed-width bitmap indexed by character position, not a field list.
    return ok_result("".join(USERS.get(g, {}).get("online", "0")
                             for g in fields(args)))


# -- news, weblinks, forums -------------------------------------------------
#
# None of these three panes has controls in a retail install, so nothing here
# is ever rendered. They answer so the sweep exercises the framing.

MOTD = ["Welcome to the local TNBrowser stand-in."]


@on("scalar", 0)
def get_motd(guid, args):
    return ok_result(MOTD[0])


@on("scalar", 4)
def set_motd(guid, args):
    return fail("You do not have moderator privileges.")


def news_row(a):
    n, t, ap, g = quad(a["author"])
    head = tab("", a["id"], a["id"], 1, date(a["created"]), a["updated"], g, "",
               n, t, ap, g, a["category"], a["headline"])
    return with_body(head, a["body"])


def _news_feed(category):
    rows = [news_row(a) for a in NEWS if not category or a["category"] == category]
    return {"status": ok_status(str(len(rows)), "0"),
            "result": str(len(rows)), "rows": rows}


@on("array", 0)
def get_news_articles(guid, args):
    return _news_feed(num(field(args, 1)))


@on("array", 100)
def get_news_by_category(guid, args):
    return _news_feed(num(field(args, 2)))


@on("scalar", 1)
def post_news_article(guid, args):
    return fail("You do not have moderator privileges.")


@on("scalar", 2)
def edit_news_article(guid, args):
    return fail("You do not have moderator privileges.")


@on("scalar", 3)
def delete_news_article(guid, args):
    return fail("You do not have moderator privileges.")


@on("array", 15)
def get_web_links(guid, args):
    # Field 0 is a per-row status: the client accepts the row only when it is
    # "0". On a non-zero query status it abandons the list entirely and falls
    # back to its own 50 hardcoded sites.
    return ok_rows([tab("0", l["name"], l["address"]) for l in WEB_LINKS])


@on("array", 7)
def get_forum_list(guid, args):
    return ok_rows([tab(i, f["name"], f["flag"], f["id"])
                    for i, f in enumerate(FORUMS)])


@on("array", 8)
def get_topic_list(guid, args):
    fid = num(field(args, 0))
    rows = []
    for t in TOPICS:
        if t["forum"] != fid:
            continue
        posts = [p for p in POSTS if p["topic"] == t["id"]]
        rows.append(tab("", t["id"], t["subject"], len(posts), "", "",
                        date(t["created"]), "",
                        USERS.get(t["author"], {}).get("name", ""),
                        "", "", "", flag(any(p["deleted"] for p in posts)),
                        0, max([p["id"] for p in posts] + [0])))
    return ok_rows(rows)


@on("array", 9)
def get_post_updates(guid, args):
    tid, since = num(field(args, 0)), num(field(args, 1))
    rows = []
    for p in POSTS:
        if p["topic"] != tid or p["id"] <= since:
            continue
        head = tab(flag(p["author"] == guid), "", p["id"], p["parent"], p["id"],
                   *quad(p["author"]), "", date(p["created"]), "",
                   flag(p["deleted"]), p["subject"])
        rows.append(with_body(head, p["body"]))
    # Status field 2 is the per-forum flag the client caches as ForumsGui.bflag.
    return {"status": ok_status("0"), "result": str(len(rows)), "rows": rows}


@on("scalar", 12)
def post_topic_or_reply(guid, args):
    POSTS.append({"id": max(p["id"] for p in POSTS) + 1,
                  "topic": num(field(args, 1)) or TOPICS[0]["id"],
                  "parent": num(field(args, 2)), "author": guid,
                  "subject": field(args, 3),
                  "body": "\n".join(fields(args)[4:]),
                  "created": NOW, "deleted": False})
    return ok_result("Posted.")


@on("scalar", 13)
def edit_post(guid, args):
    pid = num(field(args, 0))
    for p in POSTS:
        if p["id"] == pid:
            if p["author"] != guid:
                return fail("That is not your post.")
            p["subject"] = field(args, 1)
            p["body"] = "\n".join(fields(args)[2:])
            return ok_result("Updated.")
    return fail("No such post.")


@on("scalar", 14)
def post_news_or_delete_post(guid, args):
    # Genuinely ambiguous: three call sites, two argument shapes, three labels.
    # The field count is the only thing that separates them.
    if len(fields(args)) >= 3:
        return fail("You do not have moderator privileges.")
    pid = num(field(args, 0))
    for p in POSTS:
        if p["id"] == pid:
            if p["author"] != guid:
                return fail("That is not your post.")
            p["deleted"] = True
            return ok_result("Deleted.")
    return fail("No such post.")


@on("scalar", 60)
def request_topic_review(guid, args):
    return ok_result("A moderator has been notified.")


@on("scalar", 61)
def request_post_review(guid, args):
    return ok_result("A moderator has been notified.")


@on("scalar", 62)
def remove_topic(guid, args):
    return fail("Only forum staff may do that.")


@on("scalar", 66)
def lock_topic(guid, args):
    return fail("Only forum staff may do that.")


@on("scalar", 67)
def unlock_topic(guid, args):
    return fail("Only forum staff may do that.")


@on("scalar", 68)
def move_topic(guid, args):
    return fail("Only forum staff may do that.")


# --------------------------------------------------------------------------
# The certificate
# --------------------------------------------------------------------------

def certificate(guid):
    """The identity WONGetAuthInfo() hands the shipped scripts.

        record 0    name TAB tag TAB append TAB guid
        record 1    <tribe count>
        record 2+n  name TAB tag TAB append TAB tribeId TAB adminLevel TAB title
    """
    u = USERS.get(guid)
    if u is None:
        return ""
    records = [tab(u["name"], u["tag"], u["append"], guid),
               str(len(u["memberships"]))]
    # Sorted by name, because the Go backend's WarriorTribes orders that way
    # and a difference the client cannot observe is still a difference the
    # conformance run reports. Nothing in the shipped scripts depends on the
    # order of these records.
    for m in sorted(u["memberships"], key=lambda m: m["name"]):
        records.append(tab(m["name"], m["tag"], m["append"], m["id"],
                           m["rank"], m["title"]))
    return "\n".join(records)


# --------------------------------------------------------------------------
# HTTP
# --------------------------------------------------------------------------

class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    server_version = "MockTNBrowser/2.0"

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

    def _json(self, obj):
        # The blank first line the live server sends; the client trims it.
        self._send("\n" + json.dumps(obj), "application/json")

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
        if parsed.path == "/db":
            return self._db(get)
        if parsed.path == "/cert":
            return self._cert(get)
        if parsed.path == "/tn/server/authinfo":
            return self._authinfo(get)
        if parsed.path.endswith("json_browser.php"):
            return self._oracle(get)
        if parsed.path == "/healthz":
            return self._send("ok\n", "text/plain")
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

    # -- authentication ----------------------------------------------------

    def _authorised(self, get):
        guid, uuid = get("guid"), get("uuid")
        if not guid or not uuid:
            return None
        with STATE_LOCK:
            if SESSIONS.get(uuid) != guid and not self.server.open_auth:
                return None
        return guid if guid in USERS else None

    # -- the database proxy ------------------------------------------------

    def _db(self, get):
        guid = self._authorised(get)
        if guid is None:
            return self._deny(401, "<h1>Fatal Error</h1><h2>401 "
                                   "Authentication Required</h2>")

        req = {}
        raw = get("payload")
        if raw:
            try:
                req = json.loads(raw)
            except ValueError:
                return self._json(fail("The community server could not read "
                                       "that request."))

        form = req.get("form", "")
        ordinal = str(req.get("ordinal", ""))
        args = req.get("args", "")

        if form not in ("scalar", "array"):
            return self._json(fail("Unknown query form %s." % form))

        fn = ORDINALS.get((form, ordinal))
        if fn is None:
            return self._json(fail("This server does not implement %s "
                                   "ordinal %s." % (form, ordinal)))

        with STATE_LOCK:
            return self._json(fn(guid, args))

    def _cert(self, get):
        guid = self._authorised(get)
        if guid is None:
            return self._deny(401, "<h1>Fatal Error</h1><h2>401 "
                                   "Authentication Required</h2>")
        return self._json({"cert": certificate(guid)})

    def _oracle(self, get):
        """TribesNext's own verification endpoint, which this mock also stands
        in for.

        The mod no longer speaks this protocol -- it speaks ordinals -- but the
        Go backend still calls it on TribesNext to ask whether a (guid, uuid)
        pair is live, and gets the authoritative display name back in the same
        round trip. That is a third-party oracle, not a protocol we serve to
        players, and it is why the conformance run needs this route.

        "creation" is a decimal string, matching how every other timestamp
        crosses this protocol. The backend reads it as the account's
        registration date for a player it has never seen before, so without it
        a conformance run would never exercise that path.
        """
        guid = self._authorised(get)
        if guid is None:
            return self._deny(401, "<h1>Fatal Error</h1><h2>401 "
                                   "Authentication Required</h2>")
        u = USERS[guid]
        return self._json({"guid": guid, "name": u["name"],
                           "tag": u["tag"], "append": u["append"],
                           "creation": str(u["creation"]),
                           "online": u["online"], "memberships": []})

    def _authinfo(self, get):
        # Deliberately unauthenticated: a warrior name and a clan tag are on
        # the scoreboard of every server that player joins.
        cert = certificate(get("guid"))
        return self._send("\n" + cert + "\n" if cert else "\n", "text/plain")


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
    print("Mock TNBrowser backend on port %d -- %d ordinals "
          "(handshake=%s, open-auth=%s, latency=%dms)"
          % (args.port, len(ORDINALS), args.require_handshake,
             args.open_auth, args.latency),
          flush=True)
    try:
        srv.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
