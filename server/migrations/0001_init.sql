-- TNBrowser community backend -- initial schema.
--
-- Identity is not owned here. A player is identified by their TribesNext
-- account GUID, and the only proof we accept is a live TribesNext session,
-- verified upstream (see internal/auth). So there are no passwords, no
-- credentials and no key material in this database: accounts.guid is a foreign
-- identity we cache a display name against.

CREATE TABLE IF NOT EXISTS accounts (
    guid        TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    -- Which clan's tag the player currently wears. NULL means none; the column
    -- is the single source of truth for the tag shown in game.
    active_clan BIGINT,
    website     TEXT NOT NULL DEFAULT '',
    info        TEXT NOT NULL DEFAULT '',
    created     BIGINT NOT NULL,
    last_seen   BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS clans (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name        TEXT NOT NULL,
    tag         TEXT NOT NULL DEFAULT '',
    -- false: tag goes before the name; true: after. Matches the "append" field
    -- the game's auth-info record carries.
    append      BOOLEAN NOT NULL DEFAULT FALSE,
    recruiting  BOOLEAN NOT NULL DEFAULT FALSE,
    website     TEXT NOT NULL DEFAULT '',
    info        TEXT NOT NULL DEFAULT '',
    picture     TEXT NOT NULL DEFAULT '',
    active      BOOLEAN NOT NULL DEFAULT TRUE,
    created     BIGINT NOT NULL
);

-- Clan names are unique case-insensitively: "Test Clan" and "test clan" would
-- be indistinguishable in the browser's search results.
CREATE UNIQUE INDEX IF NOT EXISTS clans_name_lower ON clans (LOWER(name));

CREATE TABLE IF NOT EXISTS clan_members (
    clan_id     BIGINT NOT NULL REFERENCES clans(id) ON DELETE CASCADE,
    guid        TEXT   NOT NULL REFERENCES accounts(guid) ON DELETE CASCADE,
    -- 0 Recruit, 1 Member, 2 Officer, 3 Senior Admin, 4 Leader. The numbers are
    -- part of the wire protocol (clanrank takes "integer 0 to 4"), so they are
    -- stored as-is rather than as an enum.
    rank        SMALLINT NOT NULL DEFAULT 0 CHECK (rank BETWEEN 0 AND 4),
    title       TEXT NOT NULL DEFAULT '',
    joined      BIGINT NOT NULL,
    PRIMARY KEY (clan_id, guid)
);

CREATE INDEX IF NOT EXISTS clan_members_guid ON clan_members (guid);

CREATE TABLE IF NOT EXISTS clan_invites (
    clan_id     BIGINT NOT NULL REFERENCES clans(id) ON DELETE CASCADE,
    guid        TEXT   NOT NULL REFERENCES accounts(guid) ON DELETE CASCADE,
    from_guid   TEXT   NOT NULL,
    created     BIGINT NOT NULL,
    PRIMARY KEY (clan_id, guid)
);

CREATE INDEX IF NOT EXISTS clan_invites_guid ON clan_invites (guid);

-- Disband is an authorisation per leader, not an instant action -- the original
-- system required enough leaders to agree. A row here is one leader's vote.
CREATE TABLE IF NOT EXISTS clan_disband_votes (
    clan_id     BIGINT NOT NULL REFERENCES clans(id) ON DELETE CASCADE,
    guid        TEXT   NOT NULL,
    created     BIGINT NOT NULL,
    PRIMARY KEY (clan_id, guid)
);

-- One row per copy: sending stores a row for the recipient (folder 'inbox') and
-- one for the sender (folder 'sent'), so deleting from one mailbox cannot
-- disturb the other. Deleting moves a row to 'deleted' before it is purged.
CREATE TABLE IF NOT EXISTS mail (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner_guid  TEXT NOT NULL REFERENCES accounts(guid) ON DELETE CASCADE,
    from_guid   TEXT NOT NULL,
    from_name   TEXT NOT NULL,
    to_guid     TEXT NOT NULL,
    to_name     TEXT NOT NULL,
    subject     TEXT NOT NULL DEFAULT '',
    body        TEXT NOT NULL DEFAULT '',
    sent        BIGINT NOT NULL,
    unread      BOOLEAN NOT NULL DEFAULT TRUE,
    folder      TEXT NOT NULL DEFAULT 'inbox'
                CHECK (folder IN ('inbox', 'sent', 'deleted'))
);

CREATE INDEX IF NOT EXISTS mail_owner_folder ON mail (owner_guid, folder, sent DESC);

CREATE TABLE IF NOT EXISTS buddies (
    guid        TEXT NOT NULL REFERENCES accounts(guid) ON DELETE CASCADE,
    buddy_guid  TEXT NOT NULL,
    created     BIGINT NOT NULL,
    PRIMARY KEY (guid, buddy_guid)
);

CREATE TABLE IF NOT EXISTS blocks (
    guid         TEXT NOT NULL REFERENCES accounts(guid) ON DELETE CASCADE,
    blocked_guid TEXT NOT NULL,
    created      BIGINT NOT NULL,
    PRIMARY KEY (guid, blocked_guid)
);

-- Audit trail behind the userhistory/clanhistory methods. subject_type is
-- 'user' or 'clan'; subject_id is the GUID or clan id respectively.
CREATE TABLE IF NOT EXISTS history (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    subject_type TEXT NOT NULL CHECK (subject_type IN ('user', 'clan')),
    subject_id   TEXT NOT NULL,
    event        TEXT NOT NULL,
    at           BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS history_subject ON history (subject_type, subject_id, at DESC);
