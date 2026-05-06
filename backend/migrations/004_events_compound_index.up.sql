-- 004_events_compound_index.up.sql
-- Replace the (user_id, received_at) index with a compound index that
-- includes id, so the new compound-cursor pagination
-- (received_at, id) > ($r, $i) can be satisfied by an index scan.
DROP INDEX IF EXISTS snore_events_user_received_idx;
CREATE INDEX snore_events_user_received_id_idx
    ON snore_events (user_id, received_at, id);
