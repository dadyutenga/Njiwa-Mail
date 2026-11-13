-- 0022: one-click DNS setup (Cloudflare). Additive.
-- Provider credentials are sealed (token_enc/refresh_enc) and bound to the row
-- id; they are never returned by any API. Every row is scoped to a project.

CREATE TABLE IF NOT EXISTS dns_connections (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  provider text NOT NULL CHECK (provider IN ('cloudflare')),
  auth_method text NOT NULL CHECK (auth_method IN ('token','oauth')),
  label text NOT NULL DEFAULT '',
  token_enc bytea,
  refresh_enc bytea,
  expires_at timestamptz,
  status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','expired','revoked','disconnected','error')),
  status_detail text NOT NULL DEFAULT '',
  zone_count integer NOT NULL DEFAULT 0,
  created_by text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  last_verified_at timestamptz,
  rotated_at timestamptz,
  disconnected_at timestamptz
);
CREATE INDEX IF NOT EXISTS dns_connections_project ON dns_connections (project_id, created_at DESC);

CREATE TABLE IF NOT EXISTS dns_oauth_states (
  state_hash bytea PRIMARY KEY,
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  user_id uuid NOT NULL,
  verifier_enc bytea NOT NULL,
  redirect_uri text NOT NULL,
  domain_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  used_at timestamptz
);

CREATE TABLE IF NOT EXISTS dns_setups (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  domain_id uuid NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
  domain text NOT NULL,
  connection_id uuid REFERENCES dns_connections(id) ON DELETE SET NULL,
  zone_id text NOT NULL DEFAULT '',
  zone_name text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'analyzed' CHECK (status IN
    ('analyzed','blocked','approved','applying','applied','partial','failed','needs_review','rolled_back','rollback_partial')),
  plan jsonb NOT NULL DEFAULT '{}',
  plan_digest text NOT NULL DEFAULT '',
  approved_by text NOT NULL DEFAULT '',
  approved_at timestamptz,
  acknowledged jsonb NOT NULL DEFAULT '[]',
  applied_at timestamptz,
  created_by text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS dns_setups_domain ON dns_setups (domain_id, created_at DESC);
CREATE INDEX IF NOT EXISTS dns_setups_project ON dns_setups (project_id, created_at DESC);

-- Append-only ledger of record changes. previous/new hold DNS record fields only.
CREATE TABLE IF NOT EXISTS dns_changes (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  setup_id uuid NOT NULL REFERENCES dns_setups(id) ON DELETE CASCADE,
  project_id uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  domain text NOT NULL,
  zone_id text NOT NULL,
  item_key text NOT NULL,
  action text NOT NULL CHECK (action IN ('create','update')),
  record_id text NOT NULL DEFAULT '',
  record_type text NOT NULL,
  record_name text NOT NULL,
  previous jsonb,
  new jsonb NOT NULL,
  adopted boolean NOT NULL DEFAULT false,
  status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','applied','failed','rolled_back','rollback_skipped')),
  actor text NOT NULL DEFAULT '',
  error_code text NOT NULL DEFAULT '',
  note text NOT NULL DEFAULT '',
  applied_at timestamptz NOT NULL DEFAULT now(),
  rolled_back_at timestamptz
);
CREATE INDEX IF NOT EXISTS dns_changes_setup ON dns_changes (setup_id, applied_at);
