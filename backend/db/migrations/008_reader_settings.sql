-- Keys: mode and direction (series or parentless comic), shift (volume). An absent key is Auto or
-- not shifted.
ALTER TABLE user_to_content ADD COLUMN reader_settings JSONB NOT NULL DEFAULT '{}';
