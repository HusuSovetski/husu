BEGIN;

ALTER TABLE platforms DROP COLUMN IF EXISTS auth_type;

COMMIT;