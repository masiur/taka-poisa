-- +goose Up
-- Users who sign in only with Google have no password.
ALTER TABLE users ALTER COLUMN password_hash DROP NOT NULL;

-- External sign-in identities, keyed by the provider's stable subject id (not the email,
-- which can change on the provider side).
CREATE TABLE user_identities (
    provider   text        NOT NULL CHECK (provider IN ('google')),
    subject    text        NOT NULL,
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    email      text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, subject)
);
CREATE INDEX user_identities_user_idx ON user_identities (user_id);

-- +goose Down
DROP TABLE user_identities;
-- Google-only users cannot be represented without a password; rolling back removes them.
DELETE FROM users WHERE password_hash IS NULL;
ALTER TABLE users ALTER COLUMN password_hash SET NOT NULL;
