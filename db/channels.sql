-- LeadLoop: chat channels. One active lead per (channel, channel_user_id).
-- Safe to run more than once. Existing database: copy the file into the container, then run psql -f (see README).
-- If this fails with "could not create unique index", two active leads share the same channel id. Find them with:
--   SELECT channel, channel_user_id, count(*) FROM leads
--   WHERE channel_user_id IS NOT NULL AND anonymized_at IS NULL GROUP BY 1,2 HAVING count(*) > 1;
-- then anonymize or fix the extra rows and run this file again.
CREATE UNIQUE INDEX IF NOT EXISTS leads_channel_identity_uq
  ON leads(channel, channel_user_id)
  WHERE channel_user_id IS NOT NULL AND anonymized_at IS NULL;
