-- reading_seq orders the committed states of user_to_content rows. Every insert or update takes the
-- next value, so a larger seq is a later state of the same row; gaps are harmless. The backfill
-- draws from the same sequence, so new values start above every existing one.
CREATE SEQUENCE user_to_content_reading_seq AS bigint NO CYCLE;

ALTER TABLE user_to_content ADD COLUMN reading_seq bigint;

UPDATE user_to_content u SET reading_seq = o.seq
FROM (SELECT id, nextval('user_to_content_reading_seq') AS seq
      FROM (SELECT id FROM user_to_content ORDER BY id) ids) o
WHERE u.id = o.id;

ALTER TABLE user_to_content ALTER COLUMN reading_seq SET NOT NULL;

CREATE FUNCTION user_to_content_stamp_reading_seq() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.reading_seq := nextval('user_to_content_reading_seq');
    RETURN NEW;
END
$$;

CREATE TRIGGER user_to_content_reading_seq BEFORE INSERT OR UPDATE ON user_to_content
    FOR EACH ROW EXECUTE FUNCTION user_to_content_stamp_reading_seq();
