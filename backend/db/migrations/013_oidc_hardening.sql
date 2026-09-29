DELETE FROM auth_pending;
ALTER TABLE auth_pending ADD COLUMN attempts INT NOT NULL DEFAULT 0;
-- Stored without checking email_verified; the next login refills verified ones.
UPDATE user_identities SET email = NULL WHERE provider = 'oidc';
