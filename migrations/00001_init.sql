-- +goose Up
CREATE TABLE accounts (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       text        NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    currency   char(3)     NOT NULL DEFAULT 'BDT' CHECK (currency ~ '^[A-Z]{3}$'),
    timezone   text        NOT NULL DEFAULT 'Asia/Dhaka',
    send_hour  smallint    NOT NULL DEFAULT 9 CHECK (send_hour BETWEEN 0 AND 23),
    locale     text        NOT NULL DEFAULT 'bn' CHECK (locale IN ('bn', 'en')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email             text        NOT NULL CHECK (email = lower(email)),
    password_hash     text        NOT NULL,
    name              text        NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    locale            text        NOT NULL DEFAULT 'bn' CHECK (locale IN ('bn', 'en')),
    email_verified_at timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email_key ON users (email);

CREATE TABLE account_members (
    account_id bigint      NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role       text        NOT NULL CHECK (role IN ('owner', 'member')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, user_id)
);
CREATE INDEX account_members_user_idx ON account_members (user_id);

-- Only the SHA-256 of the session token is stored.
CREATE TABLE sessions (
    id_hash    bytea       PRIMARY KEY,
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user_idx ON sessions (user_id);

-- +goose Down
DROP TABLE sessions;
DROP TABLE account_members;
DROP TABLE users;
DROP TABLE accounts;
