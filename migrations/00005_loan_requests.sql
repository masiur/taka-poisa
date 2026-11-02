-- +goose Up
-- A loan request is an agreement in status 'requested' (direction 'borrowed', origin 'request')
-- that the lender accepts or declines.
ALTER TABLE agreements
    ADD COLUMN request_message    text CHECK (length(request_message) <= 1000),
    ADD COLUMN request_expires_at timestamptz,
    ADD COLUMN decided_at         timestamptz,
    ADD COLUMN decision_note      text CHECK (length(decision_note) <= 500),
    -- The lender's own copy (lent) when they accepted from their in-app inbox.
    ADD COLUMN lender_copy_id     bigint REFERENCES agreements (id) ON DELETE SET NULL;

-- Request links: the lender's secret page for one request.
ALTER TABLE links
    ADD COLUMN agreement_id bigint REFERENCES agreements (id) ON DELETE CASCADE,
    DROP CONSTRAINT links_kind_check,
    ADD CONSTRAINT links_kind_check CHECK (kind IN ('statement', 'request')),
    ADD CONSTRAINT links_request_has_agreement CHECK (kind <> 'request' OR agreement_id IS NOT NULL);

-- "Don't send me requests from this person": lender email blocked for one borrower account.
CREATE TABLE request_blocks (
    account_id bigint      NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    email      text        NOT NULL CHECK (email = lower(email)),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, email)
);

-- Transactional emails other than reminders (request sent, request answered). Processed by the
-- same worker with the same at-most-once rules.
CREATE TABLE outbox (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind                text        NOT NULL CHECK (kind IN ('request.created', 'request.decided')),
    account_id          bigint      NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    agreement_id        bigint      NOT NULL REFERENCES agreements (id) ON DELETE CASCADE,
    status              text        NOT NULL DEFAULT 'planned'
                        CHECK (status IN ('planned', 'sending', 'sent', 'skipped', 'failed')),
    skip_reason         text,
    attempts            int         NOT NULL DEFAULT 0,
    last_error          text,
    provider_message_id text,
    scheduled_for       timestamptz NOT NULL DEFAULT now(),
    sent_at             timestamptz,
    updated_at          timestamptz NOT NULL DEFAULT now(),
    created_at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX outbox_due_idx ON outbox (scheduled_for) WHERE status = 'planned';

-- +goose Down
DROP TABLE outbox;
DROP TABLE request_blocks;
DELETE FROM links WHERE kind = 'request';
ALTER TABLE links
    DROP CONSTRAINT links_request_has_agreement,
    DROP CONSTRAINT links_kind_check,
    ADD CONSTRAINT links_kind_check CHECK (kind IN ('statement')),
    DROP COLUMN agreement_id;
ALTER TABLE agreements
    DROP COLUMN lender_copy_id,
    DROP COLUMN decision_note,
    DROP COLUMN decided_at,
    DROP COLUMN request_expires_at,
    DROP COLUMN request_message;
