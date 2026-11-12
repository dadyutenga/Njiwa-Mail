-- F07: every webhook event carries a schema_version. An endpoint pins the
-- version it understands; each delivery is stamped with it when the event is
-- emitted, so retries and replays always keep the shape first sent.
ALTER TABLE webhook_endpoints ADD COLUMN IF NOT EXISTS schema_version int NOT NULL DEFAULT 1;
ALTER TABLE webhook_deliveries ADD COLUMN IF NOT EXISTS schema_version int NOT NULL DEFAULT 1;

CREATE OR REPLACE FUNCTION dm_emit(p_env uuid, p_type text, p_data jsonb, p_endpoint uuid DEFAULT NULL) RETURNS int
LANGUAGE sql AS $$
  WITH ins AS (
    INSERT INTO webhook_deliveries (endpoint_id, event_type, payload, schema_version)
    SELECT w.id, p_type, p_data, w.schema_version FROM webhook_endpoints w
    WHERE w.environment_id = p_env AND w.enabled AND p_type = ANY (w.events)
      AND (p_endpoint IS NULL OR w.id = p_endpoint)
    RETURNING 1)
  SELECT count(*)::int FROM ins
$$;
