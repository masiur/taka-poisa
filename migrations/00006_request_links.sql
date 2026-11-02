-- +goose Up
-- Request links: anyone with the link can ask this account to lend them money.
-- contact_id set = a link granted to one contact (their details pre-filled).
-- The token is stored as-is: it is a share link the owner copies again later, not a password;
-- revoking it is the protection.
CREATE TABLE request_links (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id bigint      NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    token      text        NOT NULL UNIQUE,
    label      text        NOT NULL DEFAULT '' CHECK (length(label) <= 100),
    contact_id bigint      REFERENCES contacts (id) ON DELETE CASCADE,
    max_amount bigint      CHECK (max_amount > 0),
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX request_links_account_idx ON request_links (account_id);

-- Requests arriving through a link are agreements in this account: direction 'lent', origin 'link'.
-- They go requested → (approved) → active once the owner marks the money as sent.
ALTER TABLE agreements
    DROP CONSTRAINT agreements_status_check,
    ADD CONSTRAINT agreements_status_check
        CHECK (status IN ('requested', 'approved', 'active', 'settled', 'declined', 'expired', 'cancelled')),
    DROP CONSTRAINT agreements_origin_check,
    ADD CONSTRAINT agreements_origin_check CHECK (origin IN ('manual', 'request', 'link')),
    ADD COLUMN request_link_id    bigint REFERENCES request_links (id) ON DELETE SET NULL,
    ADD COLUMN sent_at            timestamptz,
    ADD COLUMN sent_method        text CHECK (sent_method IN ('cash', 'bkash', 'nagad', 'rocket', 'bank', 'card', 'other')),
    ADD COLUMN sent_ref           text CHECK (length(sent_ref) <= 100),
    -- The requester's own "borrowed" copy, when they have a verified account on this server.
    ADD COLUMN borrower_copy_id   bigint REFERENCES agreements (id) ON DELETE SET NULL;

-- Where the requester wants the money. Sensitive: only the owner sees it, it is never emailed,
-- and it is deleted as soon as the request is sent, declined, withdrawn or expired.
CREATE TABLE payout_details (
    agreement_id   bigint      PRIMARY KEY REFERENCES agreements (id) ON DELETE CASCADE,
    method         text        NOT NULL CHECK (method IN ('bkash', 'nagad', 'rocket', 'bank', 'cash')),
    account_name   text        NOT NULL DEFAULT '' CHECK (length(account_name) <= 100),
    account_number text        NOT NULL DEFAULT '' CHECK (length(account_number) <= 40),
    bank_name      text        NOT NULL DEFAULT '' CHECK (length(bank_name) <= 100),
    branch         text        NOT NULL DEFAULT '' CHECK (length(branch) <= 100),
    routing_number text        NOT NULL DEFAULT '' CHECK (length(routing_number) <= 20),
    created_at     timestamptz NOT NULL DEFAULT now()
);

-- Emails this account no longer accepts link requests from.
CREATE TABLE link_blocks (
    account_id bigint      NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    email      text        NOT NULL CHECK (email = lower(email)),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, email)
);

ALTER TABLE outbox
    DROP CONSTRAINT outbox_kind_check,
    ADD CONSTRAINT outbox_kind_check
        CHECK (kind IN ('request.created', 'request.decided', 'link.received', 'link.approved', 'link.declined', 'link.sent'));

-- +goose Down
DELETE FROM outbox WHERE kind LIKE 'link.%';
ALTER TABLE outbox
    DROP CONSTRAINT outbox_kind_check,
    ADD CONSTRAINT outbox_kind_check CHECK (kind IN ('request.created', 'request.decided'));
DROP TABLE link_blocks;
DROP TABLE payout_details;
DELETE FROM agreements WHERE origin = 'link';
ALTER TABLE agreements
    DROP COLUMN borrower_copy_id,
    DROP COLUMN sent_ref,
    DROP COLUMN sent_method,
    DROP COLUMN sent_at,
    DROP COLUMN request_link_id,
    DROP CONSTRAINT agreements_origin_check,
    ADD CONSTRAINT agreements_origin_check CHECK (origin IN ('manual', 'request')),
    DROP CONSTRAINT agreements_status_check,
    ADD CONSTRAINT agreements_status_check
        CHECK (status IN ('requested', 'active', 'settled', 'declined', 'expired', 'cancelled'));
DROP TABLE request_links;
