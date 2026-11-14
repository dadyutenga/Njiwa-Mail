-- 0027: contacts, subscriber lists, segments, list-scoped unsubscribes and
-- opt-in open/click tracking. Additive.
--
-- A contact's address and name are encrypted (AAD "contact:<id>"); the
-- keyed hash finds it, the masked form and the domain are shown. Attributes
-- are plain JSON so segments can filter on them: they are for things like a
-- plan or a city, not for secrets (the dashboard says so).
CREATE TABLE IF NOT EXISTS contacts (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id       uuid NOT NULL,
  environment_id   uuid NOT NULL,
  email_hash       bytea NOT NULL,
  email_masked     text NOT NULL,
  email_domain     text NOT NULL,
  data_enc         bytea NOT NULL,          -- {"email","name"}
  attributes       jsonb NOT NULL DEFAULT '{}',
  unsubscribed_at  timestamptz,             -- from every list of this environment
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (environment_id, project_id) REFERENCES environments(id, project_id) ON DELETE CASCADE,
  UNIQUE (environment_id, email_hash)
);
CREATE INDEX IF NOT EXISTS contacts_env ON contacts (environment_id, created_at DESC);
CREATE INDEX IF NOT EXISTS contacts_attributes ON contacts USING gin (attributes);

CREATE TABLE IF NOT EXISTS contact_lists (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id      uuid NOT NULL,
  environment_id  uuid NOT NULL,
  name            text NOT NULL,
  description     text NOT NULL DEFAULT '',
  created_at      timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (environment_id, project_id) REFERENCES environments(id, project_id) ON DELETE CASCADE,
  UNIQUE (environment_id, name)
);

CREATE TABLE IF NOT EXISTS contact_list_members (
  list_id          uuid NOT NULL REFERENCES contact_lists(id) ON DELETE CASCADE,
  contact_id       uuid NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
  added_at         timestamptz NOT NULL DEFAULT now(),
  unsubscribed_at  timestamptz,             -- from this list only
  PRIMARY KEY (list_id, contact_id)
);
CREATE INDEX IF NOT EXISTS contact_list_members_contact ON contact_list_members (contact_id);

-- Dynamic lists: rules evaluated when a campaign is created.
CREATE TABLE IF NOT EXISTS contact_segments (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id      uuid NOT NULL,
  environment_id  uuid NOT NULL,
  name            text NOT NULL,
  rules           jsonb NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (environment_id, project_id) REFERENCES environments(id, project_id) ON DELETE CASCADE,
  UNIQUE (environment_id, name)
);

ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS list_id uuid REFERENCES contact_lists(id) ON DELETE SET NULL;
ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS segment_id uuid REFERENCES contact_segments(id) ON DELETE SET NULL;
ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS track_opens boolean NOT NULL DEFAULT false;
ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS track_clicks boolean NOT NULL DEFAULT false;
ALTER TABLE campaign_recipients ADD COLUMN IF NOT EXISTS contact_id uuid REFERENCES contacts(id) ON DELETE SET NULL;

-- Links of a tracked campaign, by position; a click redirects only to these.
CREATE TABLE IF NOT EXISTS campaign_links (
  campaign_id  uuid NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  idx          int NOT NULL,
  url          text NOT NULL,
  PRIMARY KEY (campaign_id, idx)
);

-- Opens and clicks. No IP address or user agent is kept.
CREATE TABLE IF NOT EXISTS tracking_events (
  id            bigserial PRIMARY KEY,
  campaign_id   uuid NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  recipient_id  bigint NOT NULL REFERENCES campaign_recipients(id) ON DELETE CASCADE,
  kind          text NOT NULL CHECK (kind IN ('open','click')),
  link_idx      int,
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS tracking_events_campaign ON tracking_events (campaign_id, kind);
