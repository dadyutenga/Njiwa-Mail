-- 0015: what an existing SMTP server sends from (backlog C07). Njiwa-Mail
-- cannot see the egress or the DKIM signer of a server it does not run, so the
-- operator states them: sending IPs and/or the provider's SPF include, the
-- DKIM selector, and the public key per domain. DNS requirements and checks
-- then use the real values instead of templates. Kept apart from the SMTP
-- settings so editing them never invalidates a connection check.
ALTER TABLE transport_profiles ADD COLUMN sending_facts jsonb NOT NULL DEFAULT '{}';
