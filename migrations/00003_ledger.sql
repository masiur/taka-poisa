-- +goose Up
CREATE TABLE contacts (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id         bigint      NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    name               text        NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    email              text        CHECK (email = lower(email)),
    phone_e164         text        CHECK (phone_e164 ~ '^\+[1-9][0-9]{6,14}$'),
    locale             text        NOT NULL DEFAULT 'bn' CHECK (locale IN ('bn', 'en')),
    notes              text        NOT NULL DEFAULT '' CHECK (length(notes) <= 2000),
    email_opted_out_at timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    deleted_at         timestamptz,
    -- Target of the composite foreign key below: an agreement's contact must be in the same account.
    UNIQUE (account_id, id)
);
CREATE INDEX contacts_account_idx ON contacts (account_id, name) WHERE deleted_at IS NULL;

-- One loan between the account and a contact. Money is in minor units (poisa).
CREATE TABLE agreements (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id bigint      NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    contact_id bigint      NOT NULL,
    direction  text        NOT NULL CHECK (direction IN ('lent', 'borrowed')),
    principal  bigint      NOT NULL CHECK (principal > 0 AND principal <= 1000000000000),
    currency   char(3)     NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    started_on date        NOT NULL,
    purpose    text        NOT NULL DEFAULT '' CHECK (length(purpose) <= 500),
    status     text        NOT NULL DEFAULT 'active'
               CHECK (status IN ('requested', 'active', 'settled', 'declined', 'expired', 'cancelled')),
    origin     text        NOT NULL DEFAULT 'manual' CHECK (origin IN ('manual', 'request')),
    created_by bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (account_id, contact_id) REFERENCES contacts (account_id, id)
);
CREATE INDEX agreements_account_idx ON agreements (account_id, status);
CREATE INDEX agreements_contact_idx ON agreements (contact_id);

-- The repayment schedule. Their amounts always sum to the agreement's principal (enforced by the app).
CREATE TABLE installments (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    agreement_id bigint NOT NULL REFERENCES agreements (id) ON DELETE CASCADE,
    seq          int    NOT NULL CHECK (seq >= 1),
    due_on       date   NOT NULL,
    amount       bigint NOT NULL CHECK (amount > 0),
    UNIQUE (agreement_id, seq)
);

-- Money actually received or paid. Only confirmed payments count towards balances;
-- installments are covered oldest first.
CREATE TABLE payments (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    agreement_id bigint      NOT NULL REFERENCES agreements (id) ON DELETE CASCADE,
    amount       bigint      NOT NULL CHECK (amount > 0),
    paid_on      date        NOT NULL,
    method       text        NOT NULL CHECK (method IN ('cash', 'bkash', 'nagad', 'rocket', 'bank', 'card', 'other')),
    note         text        NOT NULL DEFAULT '' CHECK (length(note) <= 500),
    source       text        NOT NULL DEFAULT 'owner' CHECK (source IN ('owner', 'contact_claim')),
    status       text        NOT NULL DEFAULT 'confirmed' CHECK (status IN ('confirmed', 'pending', 'rejected')),
    confirmed_at timestamptz,
    created_by   bigint      REFERENCES users (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX payments_agreement_idx ON payments (agreement_id);

CREATE TABLE activity (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id    bigint      NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    agreement_id  bigint      REFERENCES agreements (id) ON DELETE CASCADE,
    contact_id    bigint      REFERENCES contacts (id) ON DELETE CASCADE,
    actor_user_id bigint      REFERENCES users (id) ON DELETE SET NULL,
    action        text        NOT NULL,
    data          jsonb       NOT NULL DEFAULT '{}',
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX activity_agreement_idx ON activity (agreement_id, created_at);

-- +goose Down
DROP TABLE activity;
DROP TABLE payments;
DROP TABLE installments;
DROP TABLE agreements;
DROP TABLE contacts;
