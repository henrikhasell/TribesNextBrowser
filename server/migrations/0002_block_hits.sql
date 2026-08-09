-- Count of messages a block has turned away.
--
-- The stock EDIT BLOCK LIST dialog listed each blocked sender beside a
-- "# Blocked Emails" column, so the number has to be recorded somewhere. Mail
-- send already detects the block inside its transaction and returns silently;
-- it now bumps this on the way past.
ALTER TABLE blocks ADD COLUMN IF NOT EXISTS hits INTEGER NOT NULL DEFAULT 0;
