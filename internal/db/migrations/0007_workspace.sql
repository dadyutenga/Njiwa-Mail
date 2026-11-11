-- Mail workspace: domains for receiving, mailboxes, read state, drafts,
-- sent copies, temporary inboxes, automations, bulk announcements,
-- complaints, and the Mail server settings.

-- ---------------------------------------------------------------- domains
ALTER TABLE domains DROP CONSTRAINT IF EXISTS domains_purpose_check;
UPDATE domains SET purpose = 'receiving' WHERE purpose = 'inbound';
ALTER TABLE domains ADD CONSTRAINT domains_purpose_check CHECK (purpose IN ('sending','receiving','both'));
ALTER TABLE domains
  ADD COLUMN mx_ok boolean NOT NULL DEFAULT false,
  ADD COLUMN bulk boolean NOT NULL DEFAULT false,          -- separate subdomain used only for bulk announcements
  ADD COLUMN records jsonb NOT NULL DEFAULT '[]',          -- latest per-record check results
  ADD COLUMN last_checked_at timestamptz,
  ADD COLUMN mailcow_provisioned_at timestamptz;

-- ---------------------------------------------------------------- senders
ALTER TABLE sender_identities
  ADD COLUMN is_default boolean NOT NULL DEFAULT false,
  ADD COLUMN last_used_at timestamptz;
CREATE UNIQUE INDEX sender_identities_one_default ON sender_identities (environment_id) WHERE is_default;

-- ---------------------------------------------------------------- messages
-- source records how a message entered the queue. sender and recipient_masked
-- are safe metadata (our own sender, a masked recipient). subject is stored
-- only for mail an operator typed in the workspace (ui, bulk).
ALTER TABLE messages
  ADD COLUMN source text NOT NULL DEFAULT 'api' CHECK (source IN ('api','ui','bulk','forward','test','automation')),
  ADD COLUMN sender text NOT NULL DEFAULT '',
  ADD COLUMN recipient_masked text NOT NULL DEFAULT '',
  ADD COLUMN subject text NOT NULL DEFAULT '',
  ADD COLUMN campaign_id uuid,
  ADD COLUMN reply_to_inbound_id uuid;
CREATE INDEX messages_campaign ON messages (campaign_id) WHERE campaign_id IS NOT NULL;

-- A copy of what an operator composed, like a mail client's Sent folder.
-- Encrypted; removed with the message metadata by retention.
CREATE TABLE sent_copies (
  message_id uuid PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
  data_enc bytea NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE drafts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL,
  environment_id uuid NOT NULL,
  user_id uuid REFERENCES users(id) ON DELETE CASCADE,
  data_enc bytea NOT NULL,
  subject text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (environment_id, project_id) REFERENCES environments(id, project_id) ON DELETE CASCADE
);
CREATE INDEX drafts_env ON drafts (environment_id, updated_at DESC);

-- ---------------------------------------------------------------- mailboxes
ALTER TABLE inbound_addresses
  ADD COLUMN display_name text NOT NULL DEFAULT '',
  ADD COLUMN temporary boolean NOT NULL DEFAULT false,
  ADD COLUMN expires_at timestamptz;
CREATE INDEX inbound_addresses_expiry ON inbound_addresses (expires_at) WHERE expires_at IS NOT NULL;

ALTER TABLE inbound_messages
  ADD COLUMN read_at timestamptz,
  ADD COLUMN archived_at timestamptz,
  ADD COLUMN labels text[] NOT NULL DEFAULT '{}';
CREATE INDEX inbound_messages_address ON inbound_messages (address_id, received_at DESC);
CREATE INDEX inbound_messages_unread ON inbound_messages (environment_id, address_id) WHERE read_at IS NULL AND archived_at IS NULL;

CREATE TABLE blocked_senders (
  environment_id uuid NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
  sender text NOT NULL CHECK (sender = lower(sender)),
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (environment_id, sender)
);

-- ---------------------------------------------------------------- automations
CREATE TABLE automation_rules (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL,
  environment_id uuid NOT NULL,
  address_id uuid REFERENCES inbound_addresses(id) ON DELETE CASCADE,   -- NULL = every mailbox
  position int NOT NULL DEFAULT 0,
  name text NOT NULL,
  enabled boolean NOT NULL DEFAULT false,
  cond_sender text NOT NULL DEFAULT '',
  cond_subject text NOT NULL DEFAULT '',
  cond_has_attachment boolean NOT NULL DEFAULT false,
  action text NOT NULL CHECK (action IN ('forward','webhook','auto_reply','label','archive')),
  target text NOT NULL DEFAULT '',
  template_id uuid REFERENCES templates(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (environment_id, project_id) REFERENCES environments(id, project_id) ON DELETE CASCADE
);
CREATE INDEX automation_rules_env ON automation_rules (environment_id, position);

CREATE TABLE automation_runs (
  id bigserial PRIMARY KEY,
  rule_id uuid NOT NULL REFERENCES automation_rules(id) ON DELETE CASCADE,
  inbound_id uuid REFERENCES inbound_messages(id) ON DELETE SET NULL,
  result text NOT NULL,          -- done, skipped, failed, would_run, would_skip
  detail text NOT NULL DEFAULT '',
  dry_run boolean NOT NULL DEFAULT false,
  message_id uuid,
  at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX automation_runs_rule ON automation_runs (rule_id, at DESC);
CREATE INDEX automation_runs_inbound ON automation_runs (inbound_id);

-- One auto-reply per sender per rule per day.
CREATE TABLE autoreply_log (
  rule_id uuid NOT NULL REFERENCES automation_rules(id) ON DELETE CASCADE,
  sender_hash bytea NOT NULL,
  day date NOT NULL,
  PRIMARY KEY (rule_id, sender_hash, day)
);

-- ---------------------------------------------------------------- bulk announcements
ALTER TABLE projects
  ADD COLUMN bulk_max_per_campaign int NOT NULL DEFAULT 5000 CHECK (bulk_max_per_campaign >= 0),
  ADD COLUMN bulk_max_per_day int NOT NULL DEFAULT 10000 CHECK (bulk_max_per_day >= 0),
  ADD COLUMN temp_inbox_limit int NOT NULL DEFAULT 10 CHECK (temp_inbox_limit >= 0);

CREATE TABLE campaigns (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL,
  environment_id uuid NOT NULL,
  name text NOT NULL,
  sender_email text NOT NULL,
  subject text NOT NULL DEFAULT '',
  html text NOT NULL DEFAULT '',
  text_body text NOT NULL DEFAULT '',
  template_id uuid REFERENCES templates(id) ON DELETE SET NULL,
  locale text NOT NULL DEFAULT 'en',
  state text NOT NULL DEFAULT 'draft' CHECK (state IN ('draft','scheduled','sending','paused','cancelled','completed')),
  pause_reason text NOT NULL DEFAULT '',
  send_at timestamptz,
  rate_per_minute int NOT NULL DEFAULT 60 CHECK (rate_per_minute BETWEEN 1 AND 10000),
  created_by text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  started_at timestamptz,
  completed_at timestamptz,
  FOREIGN KEY (environment_id, project_id) REFERENCES environments(id, project_id) ON DELETE CASCADE
);
CREATE INDEX campaigns_env ON campaigns (environment_id, created_at DESC);
CREATE INDEX campaigns_active ON campaigns (state) WHERE state IN ('scheduled','sending');

CREATE TABLE campaign_recipients (
  id bigserial PRIMARY KEY,
  campaign_id uuid NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
  recipient_hash bytea NOT NULL,
  recipient_masked text NOT NULL,
  email_enc bytea NOT NULL,
  vars_enc bytea,
  state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','queued','skipped','failed')),
  skip_reason text NOT NULL DEFAULT '',
  message_id uuid,
  UNIQUE (campaign_id, recipient_hash)
);
CREATE INDEX campaign_recipients_pending ON campaign_recipients (campaign_id, id) WHERE state = 'pending';

-- Unsubscribes from bulk announcements only. Transactional mail is unaffected.
CREATE TABLE bulk_unsubscribes (
  environment_id uuid NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
  recipient_hash bytea NOT NULL,
  recipient_masked text NOT NULL,
  campaign_id uuid REFERENCES campaigns(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (environment_id, recipient_hash)
);

-- ---------------------------------------------------------------- complaints
ALTER TABLE bounces DROP CONSTRAINT IF EXISTS bounces_type_check;
ALTER TABLE bounces ADD CONSTRAINT bounces_type_check CHECK (type IN ('hard','soft','policy','unknown','complaint'));
ALTER TABLE suppressions DROP CONSTRAINT IF EXISTS suppressions_source_check;
ALTER TABLE suppressions ADD CONSTRAINT suppressions_source_check CHECK (source IN ('manual','hard_bounce','complaint'));

-- ---------------------------------------------------------------- mail server
-- One row. Secrets are sealed with the application key and never returned.
CREATE TABLE mail_server (
  id int PRIMARY KEY CHECK (id = 1),
  smtp_host text NOT NULL DEFAULT '',
  smtp_port int NOT NULL DEFAULT 587,
  smtp_tls_mode text NOT NULL DEFAULT 'starttls' CHECK (smtp_tls_mode IN ('starttls','implicit')),
  smtp_username text NOT NULL DEFAULT '',
  smtp_password_enc bytea,
  test_sender text NOT NULL DEFAULT '',
  inbound_listener text NOT NULL DEFAULT '',
  mailcow_api_url text NOT NULL DEFAULT '',
  mailcow_api_key_enc bytea,
  last_test jsonb,
  last_test_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now()
);
