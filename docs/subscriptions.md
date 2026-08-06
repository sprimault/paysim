> [🇬🇧 English](subscriptions.md) · [🇫🇷 Français](subscriptions.fr.md)

# Subscriptions

> **Shell examples**: `curl` snippets below assume Git Bash on Windows
> or a POSIX shell. For native Windows PowerShell, use
> `Invoke-RestMethod` with equivalent arguments (`-Method`, `-Uri`,
> `-Body`, `-ContentType`).

Paysim simulates two recurring-payment patterns supported by the
providers it emulates (PayZen today, Stripe upcoming):

- **Token pattern**: the merchant orchestrates the recurrence and
  triggers each charge on its own. Covered in
  [testing-cards.md](testing-cards.md).
- **Native subscriptions (PSP-driven)**: the merchant declares the
  schedule (`rrule`, `effectDate`) at subscription creation, and the
  PSP is expected to trigger each installment on its own. This page
  covers that second pattern.

## What Paysim does — and doesn't

Real PSPs run a **billing engine in the background** that fires each
`rrule` occurrence on the scheduled date, hits the stored payment
method, and notifies the merchant. Paysim is a simulator — a hidden
scheduler would harm the determinism a test suite needs.

Paysim's design decision: **no background scheduler**. Instead, every
installment is triggered **explicitly** through a control endpoint —
you call `POST /paysim/api/v1/subscriptions/{id}/trigger-billing`
when you want the next charge to happen. This makes CI runs
deterministic (no wall-clock dependence) and step-by-step scripting
straightforward.

The `rrule` and `effectDate` fields are **stored and returned**
faithfully (contract 3 : reproduce the protocol as-is), but never
consumed by an internal engine.

## Lifecycle

1. **Enroll a payment method** — see the token pattern in
   [testing-cards.md](testing-cards.md#magic-pan--decline-on-any-payment).
   The subscription needs a `paymentMethodToken` to bill.
2. **Create the subscription** — declare amount, currency, order id,
   effect date, rrule, metadata. Paysim assigns a `subscriptionId`.
3. **Trigger each installment** — one `trigger-billing` per period.
   Paysim creates a full `Transaction`, applies the payment-method
   checks (revoked / expired / magic PAN / magic amount), returns the
   resulting `state` (captured or declined). The link
   Transaction ↔ Subscription is stored in
   `Transaction.Metadata["subscriptionId"]` — no dedicated table.
4. **Cancel** when the merchant chooses (or the customer opts out).
   `cancelled: true` on the subscription, subsequent
   `trigger-billing` calls return `400`.

## API endpoints

All under `/paysim/api/v1/subscriptions`. Provider selection via the
`provider` field in the JSON body (defaults to `payzen`; log Debug
emitted on the default fallback).

| Method | Path                              | Purpose                                 |
| ------ | --------------------------------- | --------------------------------------- |
| POST   | `/`                               | Create — returns `{id, cancelled, …}`   |
| GET    | `/{id}`                           | Read one                                |
| GET    | `/`                               | List (per-provider filter via query)    |
| POST   | `/{id}/trigger-billing`           | Fire the next installment now           |
| POST   | `/{id}/cancel`                    | Cancel (idempotent, 204 on unknown id)  |

## Billing notification

Every `trigger-billing` emits a webhook, whether it succeeds or fails.
That is the only way a merchant learns an installment has been charged:
an installment is fired by a scheduler, never by someone waiting on the
HTTP response.

A subscription carries no notification URL of its own, so the target is
**`PAYSIM_CALLBACK_URL`**. Without that variable no notification is
emitted, and dunning becomes untestable end to end. A `WARN` log
(`fallback_callback_url`) records the target that was used, so an
unexpected delivery stays explainable.

The webhook carries the business result in its `outcome`: `PAID` on a
successful installment, `UNPAID` on a decline. A failed delivery does not
undo the installment itself, which did happen.

One-click replay (`charge_token`) follows the same rule: `notificationUrl`
when the request supplies one, `PAYSIM_CALLBACK_URL` otherwise.

## Decline conditions on `trigger-billing`

Same rules as `charge_token` — the same `decideReplayOutcome` helper
runs server-side, so the four levers documented in
[testing-cards.md](testing-cards.md#the-four-levers) apply
identically:

1. Payment method revoked (via `/payment-methods/{token}/revoke`).
2. Card expired (`expiryYear`/`expiryMonth` before current month).
3. Magic PAN (one of the four reserved test PANs).
4. Magic amount (last two digits `01`).

A cancelled subscription short-circuits the whole chain: `400` with
`abonnement annule`.

## Sample scenario

```yaml
name: subscription-monthly
description: Monthly plan — enrollment, two billings, cancellation.
steps:
  - action: create_payment
    provider: payzen
    amount: 100
    currency: EUR
    order_id: INIT
    card:
      pan: "4111111111111111"
      expiry_month: 12
      expiry_year: 2028
  - action: create_subscription
    amount: 2990
    currency: EUR
    order_id: SUB-42
    effect_date: "2026-09-01"
    rrule: "RRULE:FREQ=MONTHLY;INTERVAL=1"
    metadata:
      plan: pro
  - action: trigger_billing
  - action: assert_state
    state: captured
  - action: trigger_billing
  - action: assert_state
    state: captured
  - action: cancel_subscription
  - action: assert_subscription
    cancelled: true
```

Live equivalent in raw HTTP:

```bash
# 1. Enroll a payment method (returns {uuid, paymentMethodToken})
curl -X POST http://paysim:8080/paysim/api/v1/payments \
  -H 'Content-Type: application/json' \
  -d '{
    "provider":"payzen","amount":100,"currency":"EUR","orderId":"INIT",
    "card":{"pan":"4111111111111111","expiryMonth":12,"expiryYear":2028}
  }'

# 2. Create the subscription (returns {id, ...})
curl -X POST http://paysim:8080/paysim/api/v1/subscriptions \
  -H 'Content-Type: application/json' \
  -d '{
    "paymentMethodToken":"<TOKEN>",
    "amount":2990,"currency":"EUR","orderId":"SUB-42",
    "effectDate":"2026-09-01T00:00:00Z",
    "rrule":"RRULE:FREQ=MONTHLY;INTERVAL=1"
  }'

# 3. Trigger the next installment (returns {paymentUuid, state})
curl -X POST http://paysim:8080/paysim/api/v1/subscriptions/<ID>/trigger-billing

# 4. Cancel (204)
curl -X POST http://paysim:8080/paysim/api/v1/subscriptions/<ID>/cancel
```

## Cross-provider

The `provider` field selects the adapter — `payzen` today, `stripe`
coming later. In the meantime any request without `provider`
defaults to `payzen`. Explicit passing of the field remains valid to
future-proof integration scripts:

```bash
curl -X POST http://paysim:8080/paysim/api/v1/subscriptions \
  -d '{"provider":"payzen","paymentMethodToken":"…","amount":990,"currency":"EUR"}'
```

## Related

- [testing-cards.md](testing-cards.md) — the four decline levers and
  the token pattern (single-shot recurring).
- [ROADMAP.md](../ROADMAP.md) — phase 4 (4.4.6) covers subscriptions.
