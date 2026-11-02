-- +goose Up
-- Reminders can go by SMS (bring-your-own gateway). SMS goes to contacts only: owners have no phone here.
ALTER TABLE reminder_rules
    DROP CONSTRAINT reminder_rules_channel_check,
    ADD CONSTRAINT reminder_rules_channel_check CHECK (channel IN ('email', 'sms')),
    ADD CONSTRAINT reminder_rules_sms_to_contact CHECK (channel <> 'sms' OR recipient = 'contact');

-- SMS opt-out is separate from email opt-out.
ALTER TABLE contacts ADD COLUMN sms_opted_out_at timestamptz;

-- +goose Down
DELETE FROM reminder_sends WHERE channel = 'sms';
DELETE FROM reminder_rules WHERE channel = 'sms';
ALTER TABLE contacts DROP COLUMN sms_opted_out_at;
ALTER TABLE reminder_rules
    DROP CONSTRAINT reminder_rules_sms_to_contact,
    DROP CONSTRAINT reminder_rules_channel_check,
    ADD CONSTRAINT reminder_rules_channel_check CHECK (channel IN ('email'));
