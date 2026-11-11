-- 0011: installation-wide domain claims (backlog D01/D02).
-- Additive. Domains stay per project (each project keeps its own ownership
-- token and DNS state), but only ONE project may hold the active claim on a
-- name. Live mail requires that claim, so project B cannot send as a domain
-- project A proved, even if both added it.
--
-- Policy (D03): claims are exact names, stored as canonical lowercase ASCII
-- (IDNA/punycode, no trailing dot). A subdomain is a separate claim that needs
-- its own DNS proof; owning example.com does not grant notify.example.com.

CREATE TABLE domain_claims (
  name text PRIMARY KEY CHECK (name = lower(name) AND name NOT LIKE '%.'),
  project_id uuid REFERENCES projects(id) ON DELETE CASCADE,
  state text NOT NULL CHECK (state IN ('active','review_required')),
  claimed_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  -- An active claim has an owner; a claim under review has none until an
  -- operator assigns it.
  CHECK ((state = 'active') = (project_id IS NOT NULL))
);
CREATE INDEX domain_claims_project ON domain_claims (project_id);

-- Backfill from verified domains. A name verified by exactly one project
-- becomes that project's active claim. A name verified by several projects is
-- NOT silently given to one of them: it is flagged for operator review and no
-- project can send Live mail from it until an operator transfers it.
INSERT INTO domain_claims(name, project_id, state)
SELECT rtrim(name, '.'),
       CASE WHEN count(DISTINCT project_id) = 1 THEN (array_agg(project_id))[1] END,
       CASE WHEN count(DISTINCT project_id) = 1 THEN 'active' ELSE 'review_required' END
FROM domains
WHERE verified_at IS NOT NULL AND rtrim(name, '.') <> ''
GROUP BY rtrim(name, '.');
