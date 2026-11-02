-- +goose Up
-- Reminder rules. agreement_id NULL = the account's default set, copied onto each new agreement.
CREATE TABLE reminder_rules (
    id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id        bigint      NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    agreement_id      bigint      REFERENCES agreements (id) ON DELETE CASCADE,
    offset_days       int         NOT NULL CHECK (offset_days BETWEEN -60 AND 60),
    repeat_every_days int         CHECK (repeat_every_days BETWEEN 1 AND 30),
    repeat_count      int         CHECK (repeat_count BETWEEN 1 AND 20),
    send_hour         smallint    CHECK (send_hour BETWEEN 0 AND 23),
    recipient         text        NOT NULL CHECK (recipient IN ('contact', 'self')),
    channel           text        NOT NULL DEFAULT 'email' CHECK (channel IN ('email')),
    tone              text        NOT NULL DEFAULT 'polite' CHECK (tone IN ('polite', 'firm')),
    position          smallint    NOT NULL DEFAULT 0,
    paused_at         timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    CHECK ((repeat_every_days IS NULL) = (repeat_count IS NULL))
);
CREATE INDEX reminder_rules_agreement_idx ON reminder_rules (agreement_id);
CREATE INDEX reminder_rules_account_defaults_idx ON reminder_rules (account_id) WHERE agreement_id IS NULL;

-- Planned and past sends. This table is also the work queue: the sender claims due 'planned'
-- rows with FOR UPDATE SKIP LOCKED. At most one email per agreement, recipient and day.
CREATE TABLE reminder_sends (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id          bigint      NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    agreement_id        bigint      NOT NULL REFERENCES agreements (id) ON DELETE CASCADE,
    rule_id             bigint      REFERENCES reminder_rules (id) ON DELETE SET NULL,
    manual              boolean     NOT NULL DEFAULT false, -- "Send now"; never replaced by replanning
    recipient           text        NOT NULL CHECK (recipient IN ('contact', 'self')),
    channel             text        NOT NULL DEFAULT 'email',
    tone                text        NOT NULL CHECK (tone IN ('polite', 'firm')),
    send_day            date        NOT NULL, -- in the account's timezone
    scheduled_for       timestamptz NOT NULL,
    status              text        NOT NULL DEFAULT 'planned'
                        CHECK (status IN ('planned', 'sending', 'sent', 'skipped', 'failed')),
    skip_reason         text,
    attempts            int         NOT NULL DEFAULT 0,
    last_error          text,
    provider_message_id text,
    sent_at             timestamptz,
    updated_at          timestamptz NOT NULL DEFAULT now(),
    created_at          timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX reminder_sends_one_per_day ON reminder_sends (agreement_id, recipient, channel, send_day)
    WHERE status IN ('planned', 'sending', 'sent');
CREATE INDEX reminder_sends_due_idx ON reminder_sends (scheduled_for) WHERE status = 'planned';
CREATE INDEX reminder_sends_agreement_idx ON reminder_sends (agreement_id, scheduled_for);

-- Secret links for people without an account (statement page, opt-out). Only the SHA-256 is stored.
CREATE TABLE links (
    token_hash bytea       PRIMARY KEY,
    account_id bigint      NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    kind       text        NOT NULL CHECK (kind IN ('statement')),
    contact_id bigint      NOT NULL REFERENCES contacts (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX links_contact_idx ON links (contact_id);

-- Loans recorded before reminders existed get the built-in default rules (ledger.DefaultRules);
-- the app replans active loans on start.
INSERT INTO reminder_rules (account_id, agreement_id, offset_days, recipient, tone, position)
SELECT a.account_id, a.id, r.offset_days, r.recipient, r.tone, r.position
FROM agreements a
CROSS JOIN (VALUES (0, 'contact', 'polite', 0), (3, 'contact', 'polite', 1),
                   (7, 'contact', 'firm', 2), (-1, 'self', 'polite', 3)) AS r (offset_days, recipient, tone, position);

-- +goose Down
DROP TABLE links;
DROP TABLE reminder_sends;
DROP TABLE reminder_rules;
