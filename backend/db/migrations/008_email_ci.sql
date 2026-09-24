ALTER TABLE users DROP CONSTRAINT users_email_key;

-- Addresses that differ only in case already match nothing, so clear them and
-- let an admin set the right one.
UPDATE users SET email = NULL WHERE id IN (
    SELECT id FROM (
        SELECT id, count(*) OVER (PARTITION BY lower(email)) AS shared
        FROM users WHERE email IS NOT NULL
    ) duplicates WHERE shared > 1
);

CREATE UNIQUE INDEX users_email_lower_key ON users (lower(email));
