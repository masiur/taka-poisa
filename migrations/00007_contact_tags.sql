-- +goose Up
-- Tags group contacts (Family, Colleague, ...) inside one ledger instead of separate ledgers.
ALTER TABLE contacts
    ADD COLUMN tags text[] NOT NULL DEFAULT '{}' CHECK (cardinality(tags) <= 10);
CREATE INDEX contacts_tags_idx ON contacts USING gin (tags);

-- +goose Down
ALTER TABLE contacts DROP COLUMN tags;
