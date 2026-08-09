-- Fixtures matching tools/mockserver.py, so the client test suites can be run
-- against this backend unchanged.
--
-- The suites were written against the mock, which was written against
-- TribesNext's published PHP. Loading the same data here turns them into a
-- conformance check: anything that passes against the mock and fails here is a
-- real behavioural difference between the two backends, not a fixture accident.
--
-- Destructive. Never point this at real data.

TRUNCATE history, mail, buddies, blocks, clan_disband_votes,
         clan_invites, clan_members, clans, accounts CASCADE;

-- Fixed so "creation" dates are stable across runs.
INSERT INTO accounts (guid, name, website, info, created, last_seen) VALUES
  ('4510186', 'orange01',  'www.tribesnext.com',
   E'Testing the in-game browser.\n\nSecond paragraph with a quote: "shazbot!" and a <a:www.example.com>link</a>.',
   1785000000 - 400*86400, EXTRACT(EPOCH FROM now())::BIGINT),
  ('4120041', 'Shifter',   '', 'Long-time defender. Ask me about mortar arcs.',
   1785000000 - 900*86400, 0),
  ('4200999', 'Ravage',    'example.org', '',
   1785000000 - 120*86400, EXTRACT(EPOCH FROM now())::BIGINT),
  ('4300777', 'orangeade', '', 'Unicode check: cafe naive and a backslash \ too.',
   1785000000 -  30*86400, 0);

-- Ids are forced so the suites can refer to clans 7 and 9 as the mock does.
ALTER TABLE clans ALTER COLUMN id DROP IDENTITY IF EXISTS;

INSERT INTO clans (id, name, tag, append, recruiting, website, info, picture, active, created) VALUES
  (7, 'Test Clan',       '[TC]', FALSE, TRUE,  'www.testclan.example',
   'We are a test clan. Scrims Tuesdays.', '', TRUE, 1785000000 - 800*86400),
  (9, 'Casual Alliance', '-CA-', TRUE,  FALSE, '', '', '', TRUE, 1785000000 - 200*86400);

ALTER TABLE clans ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (START WITH 10);

INSERT INTO clan_members (clan_id, guid, rank, title, joined) VALUES
  (7, '4510186', 4, 'Leader',  1785000000 - 800*86400),
  (7, '4120041', 2, 'Officer', 1785000000 - 700*86400),
  (9, '4510186', 1, 'Member',  1785000000 - 200*86400),
  (9, '4300777', 0, 'Recruit', 1785000000 - 100*86400);

-- orange01 wears Test Clan's tag; orangeade wears Casual Alliance's.
UPDATE accounts SET active_clan = 7 WHERE guid = '4510186';
UPDATE accounts SET active_clan = 9 WHERE guid = '4300777';

-- One pending invitation for orange01 from Shifter, and one outstanding from
-- Test Clan to Ravage. The first mirrors the mock exactly (which invites a
-- player who is already a member -- odd, but it is what the suites assert on,
-- and inserting directly bypasses the membership check a real invite would hit).
INSERT INTO clan_invites (clan_id, guid, from_guid, created) VALUES
  (7, '4510186', '4120041', 1785000000 - 3600),
  (7, '4200999', '4120041', 1785000000 - 7200);

INSERT INTO history (subject_type, subject_id, event, at) VALUES
  ('user', '4510186', 'Joined clan Test Clan',      1785000000 - 86400),
  ('user', '4510186', 'Changed profile text',       1785000000 - 200000),
  ('clan', '7',       'orange01 promoted Shifter',  1785000000 - 86400),
  ('clan', '7',       'Clan created',               1785000000 - 500000);

-- Message ids are forced: the suites refer to messages 11 and 12 as the mock
-- does, and an identity sequence would hand out different numbers on every
-- reseed.
ALTER TABLE mail ALTER COLUMN id DROP IDENTITY IF EXISTS;

INSERT INTO mail (id, owner_guid, from_guid, from_name, to_guid, to_name,
                  subject, body, sent, unread, folder) VALUES
  (11, '4510186', '4120041', 'Shifter', '4510186', 'orange01',
   'Scrim on Tuesday?', 'We are short a defender. Interested?',
   1785000000 - 3600, TRUE, 'inbox'),
  (12, '4510186', '4200999', 'Ravage', '4510186', 'orange01',
   'gg', E'Good games last night.\n\n-- Ravage',
   1785000000 - 86400, FALSE, 'inbox');

ALTER TABLE mail ALTER COLUMN id ADD GENERATED ALWAYS AS IDENTITY (START WITH 100);
