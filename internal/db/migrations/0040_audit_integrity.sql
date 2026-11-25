-- Audit log integrity: a tamper-evident hash chain and append-only storage.
--
-- Every entry gets a sequence number and hash = SHA-256(previous hash || the entry's
-- canonical fields). Changing, inserting or removing any entry breaks every hash after
-- it. audit_chain_head holds the newest sequence and hash (so removing the newest entries
-- is detected too) and the anchor the oldest kept entry follows (retention moves it).
--
-- Entries are written unsealed and audit_seal() chains them in a short transaction of its
-- own, so a caller's transaction never holds the chain lock (no deadlocks, no waiting).
--
-- Sealed rows cannot be updated; no row can be deleted or truncated. The exceptions are
-- audit_seal() filling in an unsealed row's chain fields, and the retention job deleting
-- the oldest entries inside a transaction that sets njiwa.audit_retention.

ALTER TABLE audit_log ADD COLUMN seq bigint, ADD COLUMN prev_hash bytea, ADD COLUMN hash bytea;

CREATE TABLE audit_chain_head (
  singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  seq bigint NOT NULL,
  hash bytea NOT NULL,
  anchor_seq bigint NOT NULL DEFAULT 0,
  anchor_hash bytea NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);

-- The canonical form: fields joined by the unit separator, time as UTC microseconds.
CREATE FUNCTION audit_entry_hash(prev bytea, seq bigint, at timestamptz, user_id uuid, api_key_id uuid,
  actor text, action text, target_type text, target_id text, details jsonb) RETURNS bytea
LANGUAGE sql IMMUTABLE AS $$
  SELECT sha256(prev || convert_to(concat_ws(E'\x1f',
    seq::text,
    (extract(epoch FROM at) * 1000000)::bigint::text,
    coalesce(user_id::text, ''), coalesce(api_key_id::text, ''),
    actor, action, target_type, target_id, details::text), 'UTF8'))
$$;

-- Chain the entries that already exist, oldest first.
DO $$
DECLARE
  r record;
  h bytea := decode(repeat('00', 32), 'hex');
  n bigint := 0;
BEGIN
  FOR r IN SELECT * FROM audit_log ORDER BY id LOOP
    n := n + 1;
    UPDATE audit_log SET seq = n, prev_hash = h,
      hash = audit_entry_hash(h, n, r.at, r.user_id, r.api_key_id, r.actor, r.action, r.target_type, r.target_id, r.details)
    WHERE id = r.id RETURNING hash INTO h;
  END LOOP;
  INSERT INTO audit_chain_head(seq, hash, anchor_seq, anchor_hash) VALUES (n, h, 0, decode(repeat('00', 32), 'hex'));
END $$;

CREATE UNIQUE INDEX audit_log_seq ON audit_log (seq);
CREATE INDEX audit_log_action ON audit_log (action text_pattern_ops);

-- New entries arrive unsealed: nobody can supply their own chain fields.
CREATE FUNCTION audit_log_unsealed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  NEW.seq := NULL;
  NEW.prev_hash := NULL;
  NEW.hash := NULL;
  NEW.details := coalesce(NEW.details, '{}'::jsonb);
  RETURN NEW;
END $$;
CREATE TRIGGER audit_log_unsealed BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION audit_log_unsealed();
CREATE INDEX audit_log_unsealed_id ON audit_log (id) WHERE seq IS NULL;

-- audit_seal chains every committed unsealed entry, oldest id first, and returns how many.
-- The head row lock makes concurrent calls take turns; each call is short.
CREATE FUNCTION audit_seal(max_rows int DEFAULT 10000) RETURNS int LANGUAGE plpgsql AS $$
DECLARE
  head audit_chain_head%ROWTYPE;
  r record;
  prev bytea;
  n int := 0;
BEGIN
  SELECT * INTO head FROM audit_chain_head FOR UPDATE;
  PERFORM set_config('njiwa.audit_seal', 'on', true);
  FOR r IN SELECT * FROM audit_log WHERE seq IS NULL ORDER BY id LIMIT max_rows FOR UPDATE SKIP LOCKED LOOP
    prev := head.hash;
    head.seq := head.seq + 1;
    head.hash := audit_entry_hash(prev, head.seq, r.at, r.user_id, r.api_key_id, r.actor, r.action,
      r.target_type, r.target_id, r.details);
    UPDATE audit_log SET seq = head.seq, prev_hash = prev, hash = head.hash WHERE id = r.id;
    n := n + 1;
  END LOOP;
  IF n > 0 THEN
    UPDATE audit_chain_head SET seq = head.seq, hash = head.hash, updated_at = now();
  END IF;
  PERFORM set_config('njiwa.audit_seal', '', true);
  RETURN n;
END $$;

CREATE FUNCTION audit_log_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' AND current_setting('njiwa.audit_retention', true) = 'on' THEN
    RETURN OLD;
  END IF;
  -- Sealing fills in the chain fields of an unsealed row and changes nothing else.
  IF TG_OP = 'UPDATE' AND current_setting('njiwa.audit_seal', true) = 'on' AND OLD.seq IS NULL
     AND (NEW.id, NEW.at, NEW.user_id, NEW.api_key_id, NEW.actor, NEW.action, NEW.target_type, NEW.target_id, NEW.details)
       IS NOT DISTINCT FROM
         (OLD.id, OLD.at, OLD.user_id, OLD.api_key_id, OLD.actor, OLD.action, OLD.target_type, OLD.target_id, OLD.details) THEN
    RETURN NEW;
  END IF;
  RAISE EXCEPTION 'audit_log is append-only (% refused)', TG_OP USING ERRCODE = '42501';
END $$;
CREATE TRIGGER audit_log_no_update BEFORE UPDATE OR DELETE ON audit_log FOR EACH ROW EXECUTE FUNCTION audit_log_append_only();
CREATE TRIGGER audit_log_no_truncate BEFORE TRUNCATE ON audit_log FOR EACH STATEMENT EXECUTE FUNCTION audit_log_append_only();

-- The head moves only through sealing and retention.
CREATE FUNCTION audit_chain_head_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'UPDATE' AND (current_setting('njiwa.audit_seal', true) = 'on'
                           OR current_setting('njiwa.audit_retention', true) = 'on') THEN
    RETURN NEW;
  END IF;
  RAISE EXCEPTION 'audit_chain_head changes only through audit_seal() and retention (% refused)', TG_OP USING ERRCODE = '42501';
END $$;
CREATE TRIGGER audit_chain_head_guard BEFORE UPDATE OR DELETE ON audit_chain_head FOR EACH ROW EXECUTE FUNCTION audit_chain_head_guard();
CREATE TRIGGER audit_chain_head_no_truncate BEFORE TRUNCATE ON audit_chain_head FOR EACH STATEMENT EXECUTE FUNCTION audit_chain_head_guard();
