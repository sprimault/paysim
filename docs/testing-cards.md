> [🇬🇧 English](testing-cards.md) · [🇫🇷 Français](testing-cards.fr.md)

# Test cards and decline scenarios

> **Shell examples**: `curl` snippets below assume Git Bash on Windows
> or a POSIX shell. For native Windows PowerShell, use
> `Invoke-RestMethod` with equivalent arguments (`-Method`, `-Uri`,
> `-Body`, `-ContentType`).

Paysim ships four reserved test card numbers that trigger a systematic
decline on recurring charges (`charge_token`). Together with three other
independent levers, they let an integration client (CI pipelines,
manual testing) reproduce every payment failure scenario deterministically,
without depending on server state.

## Cards reserved as declines

Each brand exposes one Luhn-valid card number Paysim recognizes as a
"decline" on any `charge_token` call. Complement digits are zeros ending
with the correct Luhn check digit, keeping the value memorable and
scriptable.

| Brand              | Prefix   | Length | Test PAN            |
| ------------------ | -------- | :----: | ------------------- |
| Visa               | `400000` |  16    | `4000000000000002`  |
| Mastercard         | `510510` |  16    | `5105105105105100`  |
| Mastercard series 2| `222300` |  16    | `2223000000000007`  |
| American Express   | `378282` |  15    | `378282000000008`   |

**Scope**: recognition fires whenever the payment is linked to a
stored `PaymentMethod`. That covers both a **first payment where a
card was passed** (the simulate call declines, regardless of the
requested outcome) and a **recurring charge via a stored token**
(`charge_token` declines directly). No enrollment ceremony is
required — as soon as you attach a `card` to a `POST /payments`
call, Paysim stores it and checks the PAN at every subsequent step.

## The four levers

Each lever targets a different moment or defect and composes with the
others. All are opt-in: default behaviour is a successful payment.

| Lever          | When it acts      | Typical test case                                    |
| -------------- | ----------------- | ---------------------------------------------------- |
| Magic amount   | at `simulate`                                | Bank decline during checkout                         |
| Magic PAN      | at `simulate` **and** `charge_token`         | Bank decline on any payment using a specific card    |
| Card expiry    | at `simulate` **and** `charge_token`         | Expired card presented (mirrors real PSP behaviour)  |
| Manual revoke  | at `simulate` **and** `charge_token`         | Payment method deleted after enrolment               |

### Magic amount — decline at simulate

Any amount whose last two digits are `01` (`1001`, `2001`, `12301`, …)
forces the `simulate` endpoint to return `UNPAID`. Controls the outcome
of the browser-side checkout flow.

### Magic PAN — decline on any payment

Attach a card with one of the four PANs above at first payment. The
`simulate` call will decline even when the requested `outcome` is
`PAID`; a subsequent `charge_token` on the stored token will also
decline. Works regardless of the amount and of any prior state.

### Expired card — decline whenever presented

Attach a card with an expiry date in the past (e.g. `expiryMonth: 1,
expiryYear: 2020`). Any subsequent `simulate` or `charge_token`
declines, matching real PSP behaviour: a card is refused the moment
it's presented, not only at recurring-charge time.

**Expiry semantics** (French banking convention): a card is valid up
to and including the last day of its expiry month. `expiryMonth: 8,
expiryYear: 2026` is valid throughout August 2026 and declined from
September 1st. `IsExpired` returns true only when the current
month/year is strictly after the expiry.

### Manual revoke — decline after revocation

Call `POST /paysim/api/v1/payment-methods/{token}/revoke`. The
endpoint is idempotent (204 on unknown token). Any subsequent
`simulate` or `charge_token` referencing that payment method declines.

## Sample usage

```bash
# Enroll a card that will fail on recurring charges
curl -X POST http://paysim:8080/paysim/api/v1/payments \
  -H 'Content-Type: application/json' \
  -d '{
    "amount": 1000, "currency": "EUR", "orderId": "SUB-1",
    "formAction": "REGISTER_PAY",
    "card": {
      "pan": "4000000000000002",
      "expiryMonth": 12,
      "expiryYear": 2028,
      "holderName": "DUPONT JEAN",
      "country": "US",
      "productCategory": "DEBIT",
      "issuerName": "BANQUE DE TEST"
    }
  }'
# → {"uuid":"...","paymentMethodToken":"...","brand":"VISA"}
#   brand ships with the token so the merchant stores the real network
#   right away, instead of falling back to a default until the next
#   recurring charge.

# First payment: declines too — the magic PAN is checked at simulate,
# not only on recurring charges
curl -X POST http://paysim:8080/paysim/api/v1/payments/UUID/simulate \
  -d '{"outcome":"PAID"}'

# Recurring charge one month later: automatic decline via magic PAN
curl -X POST http://paysim:8080/paysim/api/v1/payments \
  -H 'Content-Type: application/json' \
  -d '{
    "amount": 1000, "currency": "EUR", "orderId": "SUB-1-M2",
    "paymentMethodToken": "TOKEN_FROM_ENROLL"
  }'
# → {"state":"declined", ...}
```

## Random generation, client-side

For an integration script that mixes success and failure cases at
random:

```javascript
// 90% success, 10% recurring-charge decline
const declinedPANs = [
  '4000000000000002',
  '5105105105105100',
  '2223000000000007',
  '378282000000008',
];
const pan = Math.random() < 0.9
  ? '4111' + randomDigits(12)   // any other Visa-shaped PAN, succeeds
  : declinedPANs[Math.floor(Math.random() * declinedPANs.length)];
```

Any PAN that is *not* one of the four reserved values is accepted by
Paysim as a normal card, regardless of Luhn validity — Paysim never
rejects on Luhn failure alone (it is a simulator).

## Multi-provider

The `provider` field on `POST /paysim/api/v1/payments` (and every
generic endpoint) selects the adapter. Omitting it defaults to
`payzen` — the server logs the fallback at Debug level so you can
trace implicit choices in a busy CI log.

Explicit `provider` for future-proofing (the API surface will remain
identical when Stripe joins):

```bash
# Explicit — same behaviour today, resilient to future adapters
curl -X POST http://paysim:8080/paysim/api/v1/payments \
  -H 'Content-Type: application/json' \
  -d '{
    "provider": "payzen",
    "amount": 1000, "currency": "EUR", "orderId": "O-1"
  }'

# Coming later — same endpoint, different provider
# curl -X POST http://paysim:8080/paysim/api/v1/payments \
#   -d '{"provider":"stripe","amount":1000,"currency":"EUR","orderId":"O-1"}'
```

Merchants using an official PSP SDK (Lyra client, `stripe-php`, …)
never touch this generic API — they hit the provider-native URLs
(`/api-payment/V4/*` for PayZen, `/v1/payment_intents` for Stripe).
The URL is the discriminant there; no `provider` field needed.
The generic API is meant for scenarios, UI, and integration scripts
that speak "Paysim" directly.

Details in [subscriptions.md](subscriptions.md#cross-provider).

## Telling an unusable payment method apart

A card that every charge will decline stays **registered**: that is what
lets you replay a dunning scenario on it. But it must not look like a
valid card, so `GET /paysim/api/v1/payment-methods` reports a verdict on
every entry:

```json
{ "token": "609114…", "panMasked": "400000XXXXXX0002", "revoked": false,
  "usable": false, "unusableReason": "carte de test refusee" }
```

`usable` covers the three causes at once — revoked, expired, decline
PAN — and `unusableReason` names the one that applies.

Both fields are **derived at read time, never stored**: all three causes
follow from data already held, and a frozen flag would turn wrong the
first time the month rolls over on an expiring card.

Note that a declined payment does not return a `paymentMethodToken` in
its creation response, even though the method is registered. Announcing
an alias alongside a decline would suggest it can be charged. Read the
collection to get it.

## Security reminder

**Never store real card numbers in Paysim.** The `pan` field is
persisted in cleartext, without any PCI-DSS controls, in the
`payment_methods` table (SQLite mode) or in a plain in-memory map
(memory mode). This is intentional: Paysim simulates a PSP; real cards
have no business being here.
