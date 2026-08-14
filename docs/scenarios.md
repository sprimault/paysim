> [🇬🇧 English](scenarios.md) · [🇫🇷 Français](scenarios.fr.md)

# Scenarios

Paysim ships a YAML scenario DSL and a runner (`paysim run
scenario.yml`) so integration tests can drive Paysim through a
sequence of steps and assert the outcome. Same runner in local dev,
in CI, and against a Paysim deployed in a cluster — only
`PAYSIM_URL` changes.

> **Shell examples**: `bash` snippets below assume Git Bash on Windows
> or a POSIX shell. For native Windows PowerShell, replace
> `VAR=value cmd` with `$env:VAR="value"; cmd`.

Canonical examples live in [examples/scenarios/](../examples/scenarios/) —
seven short files covering one-shot, recurring-token, subscription and
register-only patterns.

## How to run

```bash
export PAYSIM_URL=http://paysim:8080
paysim run scenarios/one-shot.yml
```

Exit codes (CI-friendly):

| Code | Meaning                                                 |
| :--: | ------------------------------------------------------- |
|  0   | All steps passed.                                       |
|  1   | Assertion failed (`assert_state`, `assert_webhook`, `assert_subscription`, `assert_payment_method`, `assert_customer`). |
|  2   | Execution error (file not found, YAML invalid, HTTP down, unknown action). |

Optional flag `--verbose` prints each step as it completes.
`PAYSIM_API_TOKEN` is picked up if set (matches the server config).

## YAML format

Every scenario is a `name`, optional `description`, and an ordered
list of `steps`. Each step carries an explicit `action` discriminator
and its own fields. This is deliberately verbose (versus a Ansible-style
implicit key) — future meta-fields (`id`, `timeout`, `retry`) stay at
the same level as `action` without breaking symmetry.

```yaml
name: my-scenario
description: What this scenario checks.
steps:
  - action: create_payment
    provider: payzen
    amount: 1000
    currency: EUR
    order_id: O-1
  - action: simulate
    status: captured
  - action: assert_state
    state: captured
```

> **Two vocabularies, not to be mixed up.** This YAML DSL uses
> `snake_case` (`order_id`, `expiry_month`, `form_action`); the HTTP API
> expects `camelCase` (`orderId`, `expiryMonth`, `formAction`).
>
> Copying a YAML field into a `curl` raises no syntax error: the unknown
> field is simply ignored. An `order_id` sent as JSON yields a payment
> with no order reference, and an `expiry_month` yields a card with no
> expiry date — now rejected with a `400`, which at least says so.

## Actions reference

Thirteen actions covering the three payment patterns.

### One-shot payments

| Action           | Purpose                                                                 |
| ---------------- | ----------------------------------------------------------------------- |
| `create_payment` | Create a payment. Optional `card`, `form_action`, `customer` (see below), `metadata`, `notification_url`. `amount: 0` valid when `form_action: REGISTER` (register-only, no debit). |
| `simulate`       | Advance the payment via the browser-return simulation endpoint.         |
| `assert_state`   | Assert the current payment is in the given state.                       |
| `assert_webhook` | Count webhooks delivered since the scenario started (optional `status`, `outcome`, `timeout`).|
| `assert_customer` | Assert the merchant context the current payment gives back, under `expect` — same shape as `customer` on `create_payment`. `uuid` optional (defaults to the last payment). Only the fields you set are compared. |

### Recurring token pattern

| Action         | Purpose                                                                     |
| -------------- | --------------------------------------------------------------------------- |
| `charge_token` | Fire a one-click recurring charge using the last enrolled `paymentMethodToken`. `token` optional (defaults to `currentToken`). |
| `assert_payment_method` | Assert what was actually stored at enrolment. `token` optional (defaults to `currentToken`). All check fields optional — only those set are compared: `brand`, `pan_masked`, `holder_name`, `country`, `product_category`, `issuer_name`, `usable`, `unusable_reason`. An assertion with no field at all is rejected at load time: it would always pass. |

### Native subscriptions (PSP-driven)

| Action                | Purpose                                                              |
| --------------------- | -------------------------------------------------------------------- |
| `create_subscription` | Register a subscription against the last enrolled payment method.    |
| `trigger_billing`     | Fire the next installment of a subscription. `subscription_id` optional.|
| `assert_subscription` | Check existence (and optionally `cancelled: true/false`).            |
| `cancel_subscription` | Cancel. Idempotent.                                                  |

### Utility

| Action  | Purpose                                                       |
| ------- | ------------------------------------------------------------- |
| `wait`  | Sleep for `duration` (`"500ms"`, `"2s"`).                     |
| `advance_time` | Move the simulator clock forward by `duration`, without sleeping. |
| `reset_time` | Return the instance to real time. No payload. |
| `inject`| Enqueue a chaos mode consumed by the **next** `simulate`.     |

`wait` and `advance_time` are exact inverses: the first sleeps without
ageing the instance — letting a delivery arrive — the second ages
without sleeping. That is what makes anything measured in days testable
in CI: an alias expiring, an instalment falling due.

```yaml
- action: advance_time
  duration: 720h        # thirty days; advances accumulate
```

Going backwards is refused at load time: a scenario running time
backwards fails before it starts. To go back, use `reset_time`.

**A scenario that advances time puts it back.** The instance is shared
across the scenarios of a single run: leaving it ahead would skew the
following ones, whose "since the scenario started" assertions would
count this one's deliveries.

```yaml
- action: advance_time
  duration: 1440h
- action: assert_payment_method
  usable: false
- action: reset_time
```

`inject` recognized modes (one-shot — consumed by the next `simulate`,
then reset):

| Mode              | Effect on the webhook triggered by the next `simulate`             |
| ----------------- | ------------------------------------------------------------------ |
| `duplicate`       | Webhook enqueued twice (test merchant idempotency).                |
| `bad-signature`   | `kr-hash` altered — merchant checking the signature must reject it.|
| `bad-algorithm`   | `kr-hash-algorithm` announces an unknown algorithm, the signature staying valid. The merchant SDK throws instead of comparing — the branch nobody tests. |
| `race`            | HTTP simulate response delayed 500 ms; the webhook fires first.    |
| `delay=NNN`       | Delay the webhook delivery by NNN milliseconds (compose with a second `simulate` to test out-of-order). |

Persistent chaos: `inject` before every `simulate` you want it to
affect. See `examples/scenarios/chaos-duplicate.yml`.

### `status` vs `outcome`

`assert_webhook` filters on two independent things, and confusing them
means asserting something other than what you think:

| Field | Answers | Values |
|---|---|---|
| `status` | *Did the webhook get through?* | `delivered`, `failed`, `pending` |
| `outcome` | *What did it announce?* | `PAID`, `UNPAID`, `AUTHORISED`… (provider vocabulary) |

A webhook delivered successfully can perfectly well announce a decline —
HTTP 200 on a `UNPAID` payload. Asserting the business result is usually
what you want:

```yaml
  - action: assert_webhook
    count: 1
    outcome: PAID          # a successful payment was announced
```

Both are cumulative: supply the two and each must match. Omit both to
count every webhook.

`outcome` is filled in by the adapter when it emits the webhook, in its
own protocol vocabulary — never re-parsed from the body. A Stripe
adapter will populate the same field with its own values.

### Why `assert_webhook` waits

Webhook delivery is asynchronous: the handler enqueues and answers, the
worker delivers and records afterwards. `assert_webhook` therefore polls
until the expected count is reached, and only fails once a deadline
expires — **5 seconds** by default. A scenario whose count is correct
exits on the first read and costs nothing.

Raise `timeout` when an `inject` has delayed delivery beyond that:

```yaml
  - action: inject
    mode: delay=8000
  - action: simulate
    status: captured
  - action: assert_webhook
    count: 1
    timeout: 12s
```

Note that `count: 0` returns immediately — it asserts that nothing has
been delivered *yet*, not that nothing ever will be.

## The `card` object

Supplying `card` on a `create_payment` enrols a payment method and
returns a reusable `paymentMethodToken`. Only the first three fields are
required:

```yaml
card:
  pan: "4111111111111111"
  expiry_month: 12
  expiry_year: 2028
  brand: VISA                  # optional, derived from the BIN if absent
  holder_name: DUPONT JEAN     # optional
  country: US                  # optional, ISO 3166-1 alpha-2, "FR" fallback
  product_category: DEBIT      # optional, CREDIT | DEBIT | PREPAID
  issuer_name: BANQUE DE TEST  # optional
```

These values are stored with the payment method and reported as-is in the
`cardDetails` block of every webhook the token later produces. That is
what makes a foreign card, a debit card, or issuer-based routing testable
— the last four fields used to be frozen at `FR` / `CREDIT` / `PAYSIM`.

**Never use a real card number**: PANs are stored in clear text. See
[testing-cards.md](testing-cards.md).

## The `customer` object

The merchant context, echoed back untouched in the webhook. Paysim never
interprets it — it only has to give it back intact, which is precisely
what scenarios are here to prove.

```yaml
customer:
  email: alice@example.com
  reference: demo-org          # merchant-side customer id
  billing_details:
    first_name: Alice
    last_name: MARTIN
    address: 1 rue de la Paix
    zip_code: "75002"
    city: Paris
    country: FR
    language: fr
  shipping_details:
    category: COMPANY          # PRIVATE | COMPANY
    legal_name: ACME SARL      # COMPANY only
    identity_code: "12345678900011"
    first_name: Bob            # the recipient is often not the payer
    last_name: DURAND
    phone_number: "+33600000000"
    street_number: "12"
    address: avenue des Champs
    address2: batiment C
    district: 8e
    zip_code: "75008"
    city: Paris
    state: IDF
    country: FR
    delivery_company_name: TRANSPORTEUR X
    shipping_speed: EXPRESS    # STANDARD | EXPRESS | PRIORITY
    shipping_method: RELAY_POINT
  extra_details:
    ip_address: 203.0.113.7
    finger_print_id: fp-abc123
    browser_user_agent: Mozilla/5.0
    browser_accept: text/html
```

Every field is optional. Names mirror PayZen's own — `shipping_details`
is split more finely than `billing_details` because PayZen's fraud rules
compare its parts one by one.

`category`, `shipping_speed` and `shipping_method` are **not validated**:
a simulator that rejected a value the real gateway accepts would be a
trap. `shipping_method` alone has some fifteen values upstream
(`RELAY_POINT`, `DIGITAL_GOOD`, `PICKUP_POINT`, `ETICKET`…), and that
list moves.

`extra_details` carries the browser context PayZen feeds to its fraud
rules and 3DS — the block to fill in when scripting a risk-based decline.

## Implicit state — one payment / one token / one subscription at a time

The runner memoises three things as the scenario runs, so most steps
can omit their `id`/`token` fields:

- `currentUUID` — updated on every `create_payment`, `charge_token`,
  `trigger_billing`. Consumed by `assert_state` and `assert_webhook`.
- `currentToken` — updated whenever a `create_payment` returns a
  `paymentMethodToken` (i.e. when a `card` was supplied). Consumed by
  `charge_token` and `create_subscription` if their `token` field is empty.
- `currentSubID` — updated on every `create_subscription`. Consumed by
  `trigger_billing`, `assert_subscription`, `cancel_subscription` if
  their `subscription_id` field is empty.

For scenarios juggling several payments/tokens/subscriptions, pass
explicit ids.

## Cross-provider

The `provider` field on `create_payment`, `charge_token` and
`create_subscription` selects the adapter — defaults to `payzen`.
When Stripe joins later (`provider: stripe`), scenarios written
today keep running unchanged; only new scenarios need to opt into the
new provider explicitly.

Details in [testing-cards.md](testing-cards.md#multi-provider) and
[subscriptions.md](subscriptions.md#cross-provider).

## Canonical scenarios

See [examples/scenarios/](../examples/scenarios/):

- `one-shot.yml` — nominal payment, capture succeeds.
- `one-shot-declined.yml` — decline through magic amount (`1001`).
- `recurring-token.yml` — merchant-orchestrated recurring charge via
  saved payment method.
- `register-only.yml` — pure card enrollment (`form_action: REGISTER`,
  `amount: 0`), plus `customer.email` and `metadata` propagation to
  the webhook, then a `charge_token` proving the saved token is usable.
  `assert_payment_method` checks the brand and cardholder attributes
  actually retained; the card is a Mastercard so the check is
  discriminating — `VISA` is the kr-answer fallback value.
- `subscription.yml` — PSP-driven subscription with two installments
  and cancellation.
- `subscription-with-decline.yml` — subscription where the recurring
  charge fails because of a magic PAN.
- `chaos-duplicate.yml` — inject `duplicate` mode, assert the webhook
  arrives twice (merchant idempotency test).

## Related

- [testing-cards.md](testing-cards.md) — the four decline levers +
  magic PANs.
- [subscriptions.md](subscriptions.md) — subscription lifecycle
  (create → trigger-billing → cancel).
- [install.md](install.md) — how to reach Paysim from your test host.
