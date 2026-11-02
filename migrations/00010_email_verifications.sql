-- +goose Up
-- Email verification links for password sign-ups. Only the SHA-256 of the token is stored;
-- the email is recorded so a link stops working if the user's email changes.
CREATE TABLE email_verifications (
    token_hash bytea       PRIMARY KEY,
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    email      text        NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX email_verifications_user_idx ON email_verifications (user_id);

-- +goose Down
DROP TABLE email_verifications;
