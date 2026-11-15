-- 0030: OpenID Connect sign-in. Additive.
-- Providers are configured by an installation operator. The client secret is
-- encrypted (AAD "sso:<id>"); the PKCE verifier of a sign-in in progress is
-- encrypted too (AAD "sso-state:<hex state hash>"). A state is single use and
-- lives ten minutes. An identity links a provider's subject to a user, so a
-- later change of the email address at the provider cannot take over another
-- account. A pending sign-in waits for the authenticator code of an account
-- that has one.
CREATE TABLE IF NOT EXISTS sso_providers (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name              text NOT NULL UNIQUE,
  issuer            text NOT NULL,
  client_id         text NOT NULL,
  client_secret_enc bytea NOT NULL,
  allowed_domains   text[] NOT NULL DEFAULT '{}',
  auto_create       boolean NOT NULL DEFAULT false,
  enabled           boolean NOT NULL DEFAULT true,
  created_at        timestamptz NOT NULL DEFAULT now(),
  CHECK (NOT auto_create OR cardinality(allowed_domains) > 0)
);

CREATE TABLE IF NOT EXISTS sso_states (
  state_hash   bytea PRIMARY KEY,
  provider_id  uuid NOT NULL REFERENCES sso_providers(id) ON DELETE CASCADE,
  nonce        text NOT NULL,
  verifier_enc bytea NOT NULL,
  expires_at   timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS sso_identities (
  provider_id  uuid NOT NULL REFERENCES sso_providers(id) ON DELETE CASCADE,
  subject      text NOT NULL,
  user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (provider_id, subject)
);

CREATE TABLE IF NOT EXISTS sso_pending (
  token_hash   bytea PRIMARY KEY,
  user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  attempts     int NOT NULL DEFAULT 0,
  expires_at   timestamptz NOT NULL
);
