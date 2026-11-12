-- D04: a sending domain may have its own return-path (bounce) domain so SPF
-- aligns with its From domain for DMARC. Empty = the installation's BOUNCE_ADDRESS domain.
ALTER TABLE domains ADD COLUMN IF NOT EXISTS bounce_domain text NOT NULL DEFAULT '';
