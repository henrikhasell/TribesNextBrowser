-- Everything the WON stored-procedure ordinals need that the TribesNext-shaped
-- schema had no reason to carry.
--
-- The client's row schemas are the specification here: a column exists below
-- because some field of some row the shipped parsers read has nowhere else to
-- come from. Where a field exists in the schema but no parser reads it, there
-- is no column -- the row is padded instead, and the ordinal table says so.

-- The warrior graphic. Field 9 of scalar 23 and the whole of scalar 31.
--
-- Never an upload: the dialog builds its list by scanning the player's own
-- texture volume for texticons/twb/twb_*.jpg (webbrowser.cs:2567), so what
-- travels is a path into data the client already has.
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS graphic TEXT NOT NULL DEFAULT '';

-- Field 0 of an array 11 row is an invite id, and the client uses it as the row
-- id in MemberList -- so it has to be stable and per-invitation, which a
-- (clan_id, guid) primary key is not.
ALTER TABLE clan_invites ADD COLUMN IF NOT EXISTS id BIGINT GENERATED ALWAYS AS IDENTITY;

-- An invitation and a join request are the same row read from opposite ends.
-- Ordinal 27 records the tribe inviting a warrior; ordinal 34 records a warrior
-- asking a tribe. Both are answered by ordinal 28 with the same three verbs,
-- and the wire cannot tell them apart -- only who is asking can.
ALTER TABLE clan_invites ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'invite'
    CHECK (kind IN ('invite', 'request'));

-- Fields 10, 13 and 14 of a mail row: the CC flag and the two recipient lists
-- the compose dialog assembled. They are display strings, not foreign keys --
-- the client shows them back verbatim and never resolves them.
ALTER TABLE mail ADD COLUMN IF NOT EXISTS is_cc   BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE mail ADD COLUMN IF NOT EXISTS to_list TEXT NOT NULL DEFAULT '';
ALTER TABLE mail ADD COLUMN IF NOT EXISTS cc_list TEXT NOT NULL DEFAULT '';

-- News: scalar 0-4, array 0 and array 100.
--
-- No controls exist for this pane in a retail install -- NewsGui is driven by
-- webnews.cs but defined in no .vl2 and no loose file -- so nothing here can be
-- rendered. It is served because the ordinals are reachable from the console
-- and answering them is cheaper than explaining why we do not.
CREATE TABLE IF NOT EXISTS news_articles (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    category    BIGINT NOT NULL DEFAULT 105,
    headline    TEXT NOT NULL,
    body        TEXT NOT NULL DEFAULT '',
    author_guid TEXT NOT NULL,
    created     BIGINT NOT NULL,
    updated     BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS news_category ON news_articles (category, created DESC);

-- One row, ever. The MOTD is a scalar the client puts straight into a text
-- control (webnews.cs:481), so it has no id and no history.
CREATE TABLE IF NOT EXISTS motd (
    only_row BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (only_row),
    text     TEXT NOT NULL DEFAULT '',
    updated  BIGINT NOT NULL DEFAULT 0
);

-- Forums: scalar 12-14, 60-63, 66-68 and array 7-9. Unrenderable for the same
-- reason as news.
CREATE TABLE IF NOT EXISTS forums (
    id       BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name     TEXT NOT NULL,
    -- Field 2 of an array 7 row, and the per-forum flag the client caches as
    -- ForumsGui.bflag out of status field 2 of array 9.
    flag     INTEGER NOT NULL DEFAULT 0,
    security INTEGER NOT NULL DEFAULT 0,
    position INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS forum_topics (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    forum_id    BIGINT NOT NULL REFERENCES forums(id) ON DELETE CASCADE,
    subject     TEXT NOT NULL,
    author_guid TEXT NOT NULL,
    created     BIGINT NOT NULL,
    updated     BIGINT NOT NULL,
    locked      BOOLEAN NOT NULL DEFAULT FALSE,
    deleted     BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS forum_topics_forum ON forum_topics (forum_id, updated DESC);

CREATE TABLE IF NOT EXISTS forum_posts (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    topic_id    BIGINT NOT NULL REFERENCES forum_topics(id) ON DELETE CASCADE,
    -- 0 for a thread root. The client normalises a post that is its own parent
    -- to 0 (webforums.cs:876); we store the normalised form.
    parent_id   BIGINT NOT NULL DEFAULT 0,
    author_guid TEXT NOT NULL,
    subject     TEXT NOT NULL DEFAULT '',
    body        TEXT NOT NULL DEFAULT '',
    created     BIGINT NOT NULL,
    deleted     BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS forum_posts_topic ON forum_posts (topic_id, id);

-- Web Links: array 15.
--
-- Seeded from weblinksmenu::defaultList (weblinks.cs:1-56), the 50 fan sites
-- the client falls back to on any non-zero status. Since the server side is
-- gone, that list is the only surviving record of what this pane served -- so
-- serving it back is the most honest answer available.
CREATE TABLE IF NOT EXISTS web_links (
    id       BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name     TEXT NOT NULL,
    address  TEXT NOT NULL,
    position INTEGER NOT NULL DEFAULT 0
);

-- Scalar 62 and 63, the eight-way moderation selectors the browser issues from
-- adjacent call sites (webbrowser.cs:1148-1211). The selector is named nowhere
-- in the shipped scripts, so the action is recorded rather than interpreted.
CREATE TABLE IF NOT EXISTS admin_actions (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_guid TEXT NOT NULL,
    ordinal    INTEGER NOT NULL,
    selector   INTEGER NOT NULL,
    args       TEXT NOT NULL DEFAULT '',
    created    BIGINT NOT NULL
);
