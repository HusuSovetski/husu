ALTER TABLE brokers
ADD COLUMN IF NOT EXISTS sm_provided_tls_credentials BOOLEAN NOT NULL DEFAULT '0';