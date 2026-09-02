-- Administrators create local accounts and reset passwords; the account holder
-- must replace a temporary password before using anything else.
ALTER TABLE users ADD COLUMN IF NOT EXISTS must_change_password boolean NOT NULL DEFAULT false;
