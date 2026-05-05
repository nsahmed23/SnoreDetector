-- 001_init.down.sql

DROP INDEX IF EXISTS snore_events_user_started_idx;
DROP INDEX IF EXISTS snore_events_user_received_idx;
DROP TABLE IF EXISTS snore_events;
DROP TABLE IF EXISTS users;
