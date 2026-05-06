DROP INDEX IF EXISTS snore_events_user_received_id_idx;
CREATE INDEX snore_events_user_received_idx
    ON snore_events (user_id, received_at);
