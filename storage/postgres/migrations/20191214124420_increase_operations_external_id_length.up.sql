BEGIN;

ALTER TABLE operations ALTER COLUMN external_id TYPE VARCHAR(10000);

COMMIT;