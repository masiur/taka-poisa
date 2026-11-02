# Taka Poisa: plan

**Date:** 2026-11-02
**Author:** Masiur (mrsiddiki@gmail.com) + Claude Opus 5.5
**Status:** Phases 0–3 built: v1 feature-complete (auth incl. Google, ledger, multiple reminders, loan requests). Phase 4 (hosted) next. Replaces the earlier Fluent Boards add-on plan.

## Summary

Taka Poisa is a money ledger ("hisab khata") for people and small businesses that **reminds the other
person for you**. You record money you lent or borrowed, split it into installments if needed, and the
app emails the other person before and after each due date until it is paid. People can also
**request a loan** through the app, and the lender accepts or declines from a link.

One codebase, two ways to run it:

- **Open source, self-hosted** (AGPL-3.0): one binary + PostgreSQL, bring your own SMTP.
- **Hosted app**: the same code with multi-account sign-up, managed email/SMS, backups and billing.

## Decisions taken

| Topic | Decision | Why |
|---|---|---|
| Language | **Go** | Single static binary, ~30 MB RAM, easiest possible self-host, fast enough by a wide margin |
| Database | **PostgreSQL** only | Date maths, row locks for jobs, Row-Level Security for hosted tenants. Supporting MySQL too would double testing for no user benefit |
| Jobs | **`reminder_sends` table as the queue** (built) | No Redis or job library: a 30-second loop claims due rows with `FOR UPDATE SKIP LOCKED`; at-most-once delivery |
| Frontend | **SvelteKit SPA**, built and embedded into the Go binary | Self-hosters still get one file; installable PWA later |
| Migrations | `goose` SQL migrations, run on start | Simple, reversible, plain SQL |
| Email | **Plain SMTP** behind a `Mailer` interface | Self-hosters use Mailjet / Brevo / Gmail / SES; hosted starts on a free tier, moves to SES |
| SMS | Phase 5, one interface; first a Bangladeshi gateway | Email-only v1 keeps scope small |
| Licence | **AGPL-3.0** | Free to self-host; nobody can run a closed competing hosted copy |
| Money | `bigint` in minor units (poisa) + ISO currency code | No floating-point rounding errors |
| Defaults | BDT, `Asia/Dhaka`, Bangla + English UI and emails | Main audience |

## Concepts

- **Account**: a ledger book. One per person on self-host; many on hosted. Has currency, timezone, send hour, language.
- **Contact**: the other person (name, phone, email). Does not need an account.
- **Agreement**: one loan between the account and a contact. Direction is `lent` (they owe me) or `borrowed` (I owe them).
- **Installment**: a dated amount within an agreement. A lump sum is an agreement with one installment.
- **Payment**: money actually received or paid, full or partial. Applied to the oldest unpaid installment first.
- **Reminder rule**: one reminder ("3 days before", "on the due date", "every 3 days while late"), with its own
  recipient, channel, time and message tone. An agreement has **any number of rules** (up to 10).
- **Loan request**: a proposed agreement waiting for the lender's answer.
- **Statement link**: a secret, read-only page the contact opens without logging in.

## Features

### v1 (open source release)

1. **Contacts**: add/edit, notes, per-contact balance.
2. **Agreements**: lent or borrowed, amount, start date, purpose/note.
3. **Repayment plan**: lump sum or N installments (monthly, every N days, or custom dates).
   Monthly dates clamp to month end (Jan 31 → Feb 28 → Mar 31). Amount split puts the remainder on the last installment.
4. **Payments**: record full or partial payments, with method (cash, bKash, Nagad, bank, other) and note.
5. **Balances**: per agreement, per contact, and totals ("You'll get 45,000 · You owe 8,000"), plus overdue amounts.
6. **Multiple reminders per agreement** (see the reminder engine section): a list of reminder rules, added,
   edited, paused or removed one by one, sent by email:
   - to the contact when they owe you (`lent`);
   - to yourself when you owe someone (`borrowed`), or when someone's installment is overdue;
   - each rule can have its own time of day and tone (polite / firm), so the first reminder can be gentle and later ones firmer.
   Presets fill the list in one tap; the account keeps a **default rule set** copied onto every new agreement.
   A **"Send reminder now"** button sends one immediately, outside the rules.
7. **Statement link**: the schedule, payments and remaining balance; works for all of a contact's agreements.
8. **"I've paid" claim** on the statement page: the contact enters amount + method + note; it shows as
   *pending* until the owner confirms, then becomes a payment. Unconfirmed claims never change balances.
9. **Loan requests** (see the loan requests section).
10. **Opt-out** link in every email; opted-out contacts get no more emails. The owner sees why.
11. **Activity log** per agreement: created, edited, payment recorded, reminder sent/failed, claim confirmed.
12. **CSV export** of contacts, agreements, installments and payments.
13. **Email templates** in Bangla and English, with a polite and a firmer tone; per-contact language.

### Later (phase 5+)

- SMS via a Bangladeshi gateway (SSL Wireless, Alpha SMS, BulkSMSBD…); Twilio for other countries.
- Recurring obligations (rent, membership dues, tuition) with no end date.
- Payment instructions on the statement page (bKash/Nagad number, payment link).
- Receipt PDF when an agreement is settled.
- CSV import; installable PWA with push reminders to the owner.
- Linked ledgers: when both people use Taka Poisa, keep both sides of an agreement in sync.
- Public "request money from me" page per user (needs spam control).

### Hosted-only

- Managed email at higher limits, SMS credits, WhatsApp reminders (Meta business verification).
- Custom sending domain, automatic backups, team books (shop owner + staff with roles).

### Explicitly out of scope

Interest/EMI calculation, group expense splitting, bank syncing, accounting/tax reports,
taking payments inside the app. Each is a different product.

## Loan requests

A request is an agreement in status `requested` with one extra rule: the **lender** decides.

### Flow A: I ask someone for a loan (lender has no account)

1. Borrower creates a request: contact (lender), amount, purpose, proposed repayment plan, optional message.
2. The lender gets an email with a secret link to a request page: amount, purpose, proposed installments, borrower's message.
3. The lender chooses **Accept** or **Decline** (optional note). v1 has no counter-offer; the lender can reply by email
   (Reply-To is the borrower).
4. Accept → the agreement becomes `active` in the borrower's book as `borrowed`, with the proposed
   installments. Reminders to the borrower (self) start from the schedule. Decline → status `declined`, borrower is notified.
5. The lender's accept page offers "Create a free Taka Poisa account to track this", which creates
   their own `lent` copy (no sync in v1).

Built note: the in-app inbox (Flow B) only shows requests to users whose email is **verified** (today: signed in
with Google using that email), so registering someone else's email never exposes their requests. Phase 4 must
add email verification for password sign-ups before opening sign-up on the hosted app.

### Flow B: someone with an account on the same instance asks me

Same as A, but the request also appears in the lender's in-app **Requests** inbox, and accepting
creates the lender's `lent` agreement directly in their book.

### Rules

- A request expires after 14 days (configurable per request, 1–60) → status `expired`.
- The borrower can cancel while it is `requested`; the link then shows "This request was withdrawn".
- No money moves through the app; acceptance is a record that both sides agreed, with timestamp and IP stored in the activity log.
- Rate limits: max 10 open requests per account, max 3 requests per lender email per 7 days (stops spam/harassment).
- The lender can click "Don't send me requests from this person" (blocks that account → that email).

## Data model (PostgreSQL)

All tenant tables carry `account_id`; on hosted, RLS policies enforce `account_id = current_setting('app.account_id')`.
The app also scopes every query by account, so self-host without RLS is still safe.

```sql
accounts(id, name, currency char(3), timezone, send_hour smallint, locale, created_at)
users(id, email citext unique, password_hash, name, locale, email_verified_at, created_at)
account_members(account_id, user_id, role)                -- owner | member (hosted teams later)
sessions(id_hash, user_id, expires_at, created_at)

contacts(id, account_id, name, email citext, phone_e164, locale, notes,
         email_opted_out_at, blocked_requests_at, created_at, updated_at, deleted_at)

agreements(id, account_id, contact_id, direction,          -- lent | borrowed
           principal bigint, currency char(3), started_on date, purpose,
           status,                                         -- requested | active | settled | declined | expired | cancelled
           origin,                                         -- manual | request
           request_expires_at, decided_at, decision_note,
           created_by, created_at, updated_at)

installments(id, agreement_id, seq, due_on date, amount bigint)   -- unique(agreement_id, seq)

payments(id, agreement_id, amount bigint, paid_on date, method, note,
         source,                                           -- owner | contact_claim
         status,                                           -- confirmed | pending | rejected
         confirmed_at, created_at)

-- Any number of rules per agreement (app limit 10). account_id + agreement_id NULL = the account's
-- default rule set, copied onto each new agreement.
reminder_rules(id, account_id, agreement_id NULL,
               offset_days int,                            -- -60..60 relative to the due date: -3 = 3 days before, 0 = on the day, 2 = 2 days late
               repeat_every_days int NULL,                 -- 1..30: keep repeating after the offset while unpaid
               repeat_count int NULL,                      -- 1..20: how many repeats (bounded, never endless)
               send_hour smallint NULL,                    -- override of the account's send hour
               recipient,                                  -- contact | self
               channel,                                    -- email (sms later)
               tone,                                       -- polite | firm
               position smallint, paused_at, created_at)

reminder_sends(id, agreement_id, installment_id, rule_id NULL,   -- NULL = sent manually ("Send now")
               recipient, channel, scheduled_for timestamptz,
               status,                                     -- planned | sent | skipped | failed | cancelled
               skip_reason, provider_message_id, sent_at,
               unique(installment_id, rule_id, recipient, channel, scheduled_for))

links(token_hash, account_id, kind,                        -- statement | request | optout
      contact_id, agreement_id, expires_at, revoked_at, created_at)

activity(id, account_id, agreement_id, actor, action, data jsonb, ip, created_at)
```

**Derived, not stored:** paid amount per installment, installment status (paid / partly paid / due / overdue),
balances. They come from confirmed payments applied oldest-installment-first, in one SQL view
or Go function. Storing them would let them drift.

Agreement becomes `settled` automatically when confirmed payments ≥ total of installments,
and back to `active` if a payment is deleted.

## Reminder engine

Plan the sends as rows instead of computing them on every tick:

1. **Plan**: whenever an agreement's installments, rules, payments or the contact's opt-out change,
   one function deletes that agreement's `planned` rows and regenerates them:
   for each unpaid installment × each active rule: `due_on + offset_days`, then every `repeat_every_days`
   for `repeat_count` more, at the rule's `send_hour` (or the account's) in the account timezone.
   Two rules landing on the same day for the same recipient and channel collapse into one email
   (the firmer tone wins), so overlapping rules never double-message anyone.
   Past times are not planned (no flood of catch-up emails after downtime beyond the most recent one).
2. **Send**: a worker loop (every 30 s) claims `planned` rows with `scheduled_for <= now()` using
   `FOR UPDATE SKIP LOCKED` and marks them `sending`. A row stuck in `sending` (crash) is marked failed,
   never resent: reminders are delivered at most once. Rows more than 24 h late are skipped as `missed`.
3. **Check before sending**: agreement still active, installment still unpaid, schedule not paused,
   recipient has an address and hasn't opted out. Otherwise `skipped` with a reason.
4. **Send + record**: send via `Mailer`, store `provider_message_id`, write activity. On failure it is requeued
   with backoff (3 attempts), then `failed`, shown on the agreement.

The unique key on `reminder_sends` and the row lock make double-sending impossible, even with several app instances.
The planned rows also give the UI "Next reminder: 30 Jan, 09:00" for free.

Presets fill the rule list (then each rule can be edited):
- **Gentle**: on the due date; 3 days late; 7 days late.
- **Firm**: 3 days before; on the due date; 1 day late, then every 3 days ×5 (firm tone).
- **Early bird**: 7 days before; 1 day before; on the due date.

Hard caps keep a bad configuration from spamming anyone: at most 10 rules per agreement, 30 planned sends
per installment, and one email per recipient per day per agreement.

## Email

- `Mailer` interface with one SMTP implementation (`net/smtp` + STARTTLS/TLS) for v1. Configured by env vars.
- From: the instance's address (`reminders@yourdomain`), display name "Rahim via Taka Poisa", **Reply-To: the owner**.
  Never send as the owner's own address (fails SPF/DMARC, lands in spam).
- Every email: plain-text + HTML parts, statement/request link button, opt-out link, `List-Unsubscribe` header.
- Self-host docs cover Mailjet, Brevo, Gmail SMTP and SES setup, plus SPF/DKIM for your domain.
- Hosted: start on Mailjet/Brevo free tier (check terms allow multi-tenant sending), move to Amazon SES before a few
  hundred active accounts. Per-account daily send caps protect the shared reputation.

Free-tier limits (2026-11, they change): Mailjet ~200/day, Brevo ~300/day, Resend ~100/day; SES ~$0.10 per 1,000.

## Self-hosted vs hosted

Same binary. Behaviour switches by config, not by a separate codebase.

| | Self-hosted | Hosted |
|---|---|---|
| Sign-up | First visitor creates the owner; then invite-only (`SIGNUP=closed`) | Open, with email verification |
| Accounts | One (more possible) | Many, RLS on |
| Email | Your SMTP | Managed, per-account caps by plan |
| SMS / WhatsApp | Your gateway keys (later) | Credits |
| Backups | Your job (`pg_dump` doc) | Managed |
| Billing | None | Plans + payment gateway (decide: SSLCommerz / bKash / Stripe) |

Deploy for self-host: `docker compose up` (app + Postgres), or the binary + an existing Postgres.
Config entirely via env: `DATABASE_URL`, `BASE_URL`, `SMTP_*`, `SIGNUP`, `SECRET_KEY`.

## Security & privacy

This is financial data about people who never signed up, so:

- Passwords with argon2id; session cookies `HttpOnly`, `Secure`, `SameSite=Lax`; CSRF token on state-changing requests.
- Link tokens: 32 random bytes, only the SHA-256 hash stored; statement links revocable; request links expire.
- Never put email, phone or amounts in URLs or email subjects beyond the first name.
- Rate limits on login, public link pages, claims and loan requests.
- Server-side validation of all input: amounts > 0 and ≤ 10^12 minor units, installments 1–120,
  `before_days` 0–60, `overdue_every_days` 0–30, `max_sends` 1–20, dates within ±10 years.
- Every account can export all its data (CSV) and delete itself (hard delete after 30 days).
- Contacts can opt out from any email; opt-out is honoured by the reminder check, not just the UI.

## Repository layout

```
cmd/takapoisa/        main: config, migrate, HTTP server, reminder worker
internal/ledger/      agreements, installments, payments, balance maths
internal/reminders/   email templates and the sender worker (planner lives in internal/ledger)
internal/requests/    loan request flow
internal/mail/        Mailer interface, SMTP, templates (bn, en)
internal/auth/        users, sessions, accounts
internal/http/        handlers, middleware, public link pages
migrations/           goose SQL files
web/                  SvelteKit app (built into internal/http/static via go:embed)
Dockerfile, compose.yaml (repo root)
docs/                 self-hosting, SMTP setup, plan
```

## Phases

| Phase | Scope | Done when |
|---|---|---|
| 0. Skeleton | Go module, config, migrations, auth (signup/login/logout), account settings, Docker, CI (tests + lint) | `docker compose up` → create owner → log in |
| 1. Ledger | Contacts, agreements, installment generator, payments, balances, activity, CSV export | Lend 30,000 in 3 installments, record a partial payment, balances correct |
| 2. Reminders | Multiple reminder rules + presets + account defaults + send-now, planner, sender, email templates bn/en, statement link, "I've paid" claim, opt-out, self reminders | Reminders arrive on time; stop on payment, opt-out, pause and max sends |
| 3. Loan requests | Flows A and B, expiry, cancel, rate limits, block | Request → email → accept → agreement with reminders |
| **v1.0 release** | Docs, self-host guide, AGPL licence | |
| 4. Hosted | Open sign-up, RLS, per-plan limits, managed email, billing, backups | Paid account on the hosted app |
| 5. Extensions | SMS, recurring obligations, receipts, import, PWA, linked ledgers | By demand |

## Verification

- **Unit tests (Go)**: installment generation (month-end, remainder rounding, custom dates), payment allocation
  (partial, overpayment, deletion), balance totals, reminder planning for each preset and for overlapping rules (same-day collapse, caps), request state transitions.
- **Integration tests** against a real Postgres (testcontainers): planner + sender with a fake clock and fake `Mailer`;
  two workers running at once → exactly one email per planned row; RLS blocks cross-account reads on hosted mode.
- **Scenarios**:
  - Lump sum: reminder 3 days before, on the due date, overdue repeats; payment → remaining planned rows cancelled.
  - Contact without email → `skipped`, agreement shows why. Opt-out → no further emails.
  - Claim "I've paid 5,000" → balance unchanged until confirmed.
  - Loan request accepted → agreement active with installments; declined/expired/cancelled → no agreement, link shows status.
- **Manual**: run with Mailpit as the SMTP server and check rendering of every template in both languages.

## Open decisions

1. Hosted billing gateway: SSLCommerz, bKash, Stripe, or a mix.
2. First SMS gateway for phase 5 (needs BTRC sender ID).
3. Product domain / sending domain for the hosted app.
4. Counter-offers on loan requests (v1 says no; revisit after feedback).
5. Linked ledgers between two users: sync model and conflict rules (phase 5).
