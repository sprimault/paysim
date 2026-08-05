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
|  1   | Assertion failed (`assert_state`, `assert_webhook`, `assert_subscription`). |
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

Field names use `snake_case` in YAML, `camelCase` on the wire (JSON API).

## Actions reference

Eleven actions covering the three payment patterns.

### One-shot payments

| Action           | Purpose                                                                 |
| ---------------- | ----------------------------------------------------------------------- |
| `create_payment` | Create a payment. Optional `card`, `form_action`, `customer.email`, `metadata`, `notification_url`. `amount: 0` valid when `form_action: REGISTER` (register-only, no debit). |
| `simulate`       | Advance the payment via the browser-return simulation endpoint.         |
| `assert_state`   | Assert the current payment is in the given state.                       |
| `assert_webhook` | Count webhooks delivered since the scenario started (optional `status`).|

### Recurring token pattern

| Action         | Purpose                                                                     |
| -------------- | --------------------------------------------------------------------------- |
| `charge_token` | Fire a one-click recurring charge using the last enrolled `paymentMethodToken`. `token` optional (defaults to `currentToken`). |

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
| `inject`| Enqueue a chaos mode consumed by the **next** `simulate`.     |

`inject` recognized modes (one-shot — consumed by the next `simulate`,
then reset):

| Mode              | Effect on the webhook triggered by the next `simulate`             |
| ----------------- | ------------------------------------------------------------------ |
| `duplicate`       | Webhook enqueued twice (test merchant idempotency).                |
| `bad-signature`   | `kr-hash` altered — merchant checking the signature must reject it.|
| `race`            | HTTP simulate response delayed 500 ms; the webhook fires first.    |
| `delay=NNN`       | Delay the webhook delivery by NNN milliseconds (compose with a second `simulate` to test out-of-order). |

Persistent chaos: `inject` before every `simulate` you want it to
affect. See `examples/scenarios/chaos-duplicate.yml`.

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
