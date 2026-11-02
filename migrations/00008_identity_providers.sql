-- +goose Up
-- Outside sign-in providers are pluggable (the hosted app adds Google); accept any short provider id.
ALTER TABLE user_identities
    DROP CONSTRAINT user_identities_provider_check,
    ADD CONSTRAINT user_identities_provider_check CHECK (provider ~ '^[a-z][a-z0-9_]{1,30}$');

-- +goose Down
DELETE FROM user_identities WHERE provider <> 'google';
ALTER TABLE user_identities
    DROP CONSTRAINT user_identities_provider_check,
    ADD CONSTRAINT user_identities_provider_check CHECK (provider IN ('google'));
