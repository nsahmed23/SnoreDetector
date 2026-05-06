-- 007_audio_clips.down.sql
DROP INDEX IF EXISTS audio_clips_user_active_idx;
DROP INDEX IF EXISTS audio_clips_user_session_idx;
DROP INDEX IF EXISTS audio_clips_user_started_idx;
DROP TABLE IF EXISTS audio_clips;
