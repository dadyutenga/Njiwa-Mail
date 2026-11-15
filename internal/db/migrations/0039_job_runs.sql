-- 0039: what each scheduled job last did. Additive. Written by the scheduler process.
CREATE TABLE IF NOT EXISTS job_runs (
  name             text PRIMARY KEY,
  interval_seconds int NOT NULL CHECK (interval_seconds > 0),
  registered_at    timestamptz NOT NULL DEFAULT now(),
  last_started_at  timestamptz,
  last_finished_at timestamptz,
  last_ok          boolean,
  last_error       text NOT NULL DEFAULT '',
  last_duration_ms int NOT NULL DEFAULT 0,
  runs             bigint NOT NULL DEFAULT 0,
  failures         bigint NOT NULL DEFAULT 0
);
