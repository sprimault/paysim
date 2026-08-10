> [🇬🇧 English](payzen.md) · [🇫🇷 Français](payzen.fr.md)

# PayZen provider reference

Exhaustive reference of the PayZen protocol as simulated by Paysim.
Documents every native endpoint, every request/response field, every
allowed value, and where Paysim behaves differently from the real
PSP — plus the Paysim-specific extensions layered on top.

Source of truth for the code side: `internal/providers/payzen/types.go`
and `handler.go`. This document is manually kept in sync — a
divergence with the code is a bug in the doc, not a spec.

## Overview

**Base URL** (Paysim):
`http://paysim:8080/api-payment/V4/*` — mirrors the real PayZen
`https://api.payzen.eu/api-payment/V4/*`. A merchant switching from
PayZen sandbox to Paysim only changes the base host.

**Authentication**: HTTP Basic. Any non-empty user:pass pair is
accepted (permissive Basic Auth — the simulator does not gate real
access, it only logs the user for observability).

**Envelope**: every response follows the PayZen envelope:
```json
{
  "status": "SUCCESS" | "ERROR",
  "answer": { … }
}
```
The `answer` blob differs by endpoint. On errors, `answer` is an
`APIError` (see [Error codes](#error-codes)).

**Content type**: `application/json` on request, same on response.

**Reference upstream**:
[PayZen REST V4 docs](https://payzen.io/en-EN/rest/V4.0/api/) and
[Lyra Collect docs](https://docs.lyra.com/en/rest/V4.0/api/).

## Endpoint coverage

### Simulated

| Method | Path                                    | Answer type                 |
| ------ | --------------------------------------- | --------------------------- |
| POST   | `/api-payment/V4/Charge/CreatePayment`  | `CreatePaymentAnswer`       |
| POST   | `/api-payment/V4/Charge/UpdatePayment`  | `UpdatePaymentAnswer`       |
| POST   | `/api-payment/V4/Charge/CreateSubscription` | `CreateSubscriptionAnswer` |
| POST   | `/api-payment/V4/Transaction/Get`       | `TransactionGetAnswer`      |
| POST   | `/api-payment/V4/Subscription/Get`      | `SubscriptionGetAnswer`     |

### Not simulated (out of scope today)

Explicit non-goals — endpoints that a real PayZen client can call
against Paysim, that will return `404` or an unmodelled error. Ask
for coverage if you actually need one:

- `Transaction/Refund`, `Transaction/CancelOrRefund`, `Transaction/Update`
- `Subscription/Update`, `Subscription/Cancel` (native — Paysim's
  cancel lives on the generic API, `POST /paysim/api/v1/subscriptions/{id}/cancel`)
- `Charge/SDKTest` (SDK diagnostics), `Charge/CreateToken` (token-only creation)
- `PCI/Charge/CreatePayment` (server-side card entry, PCI scope)
- `Wallet/*`, `Customer/*`, `Order/*`, `Session/*`

## Native endpoints — detailed

### Charge/CreatePayment

Creates a payment context and returns a `formToken` the merchant hands
to the SmartForm JavaScript client (real PayZen), or reuses via
`paymentMethodToken` for a one-click charge (Paysim extension, see
below).

**Request** — `CreatePaymentRequest`:

| Field              | Type                | Required | Notes                                                       |
| ------------------ | ------------------- | :------: | ----------------------------------------------------------- |
| `orderId`          | string              |    yes   | Merchant order reference, free-form.                        |
| `amount`           | integer (cents)     |    yes   | In smallest currency unit (cents for EUR).                  |
| `currency`         | string (ISO 4217)   |    yes   | Three uppercase letters (`EUR`, `USD`, …).                  |
| `formAction`       | string              |    no    | See allowed values below.                                   |
| `customer`         | `Customer`          |    no    | Buyer info — `email`, `reference`, `billingDetails`. `reference` is the merchant-side customer id, echoed back in the `kr-answer`. |
| `metadata`         | `map[string]string` |    no    | Free-form merchant metadata, echoed in webhook.             |
| `returnUrl`        | string              |    no    | Paysim extension: browser-return target.                    |
| `notificationUrl`  | string              |    no    | Paysim extension: IPN webhook target.                       |
| `paymentMethodToken` | string           |    no    | **Paysim / PayZen extension**: one-click recurring charge from a saved payment method. |
| `card`             | `Card`              |    no    | **Paysim extension only** (real PayZen collects card via SmartForm client). Enrolls a payment method server-side. |

`formAction` allowed values:

| Value              | Behaviour                                                             |
| ------------------ | --------------------------------------------------------------------- |
| `PAYMENT` (default)| One-shot payment, no enrollment.                                      |
| `REGISTER_PAY`     | Payment + mandatory enrollment of the payment method.                 |
| `ASK_REGISTER_PAY` | Payment + enrollment proposed to the user.                            |
| `REGISTER`         | Enrollment only, no debit. `amount: 0` accepted — creates a `paymentMethodToken` reusable for one-click charges. |

**Paysim specifics**:

- The `card` field is **not part of the real PayZen contract** — in
  production the card data transits via the SmartForm client
  (`kr-payment-form.min.js`), never through the merchant API. Paysim
  accepts it as an integration convenience. **The alias is only created
  once the authorization is accepted**, as with PayZen — "the alias
  (token) will not be created if the authorization or information
  request is declined":

  | Amount | What happens | Payment state | When the alias appears |
  | --- | --- | --- | --- |
  | `0` | Verification, no debit | `authorized` if accepted, `declined` otherwise | Immediately, in the creation response |
  | `> 0` | A debit to play | `initiated` until `simulate` | At `simulate`, if the outcome is accepted |

  The verification is **authorized, never captured**: at Lyra this
  transaction "is never settled and stays in the Transactions in
  progress tab". There was an authorization request, no movement of
  funds.

  A declined payment therefore leaves **no** alias, and the creation
  response of a `REGISTER_PAY` carries no token yet — read the payment
  back, or read it from the webhook. See
  [testing-cards.md](../testing-cards.md).
- `paymentMethodToken` in the request triggers **one-click recurring
  charge** mode: no form, direct capture (or decline), synchronous
  outcome, IPN webhook emitted.
- **Magic amount `xxx03`** applies latency before response (30 s),
  simulating a timeout without changing outcome (see
  [chaos values](#chaos-values-magic-values)).

**Response** — `CreatePaymentAnswer`:

```json
{
  "status": "SUCCESS",
  "answer": { "formToken": "<32 hex chars>" }
}
```

`formToken` is opaque merchant-side, 32 hex chars, generated by Paysim.
The merchant hands it to the SmartForm; it is unrelated to
`paymentMethodToken`.

### Charge/UpdatePayment

Updates the context of an existing payment (typically `customer` after
UI edits, or `metadata`). Does not touch the domain state.

**Request** — `UpdatePaymentRequest`:

| Field       | Type                | Required |
| ----------- | ------------------- | :------: |
| `formToken` | string              |    yes   |
| `customer`  | `Customer`          |    no    |
| `metadata`  | `map[string]string` |    no    |

**Response** — same `formToken` (unchanged).

### Charge/CreateSubscription

Creates a native PSP-driven subscription. The subscription runs no
scheduler in the background on Paysim — see
[subscriptions.md](../subscriptions.md) for the deliberate
choice.

**Request** — `CreateSubscriptionRequest`:

| Field                | Type                | Required | Notes                                        |
| -------------------- | ------------------- | :------: | -------------------------------------------- |
| `paymentMethodToken` | string              |    yes   | Must reference an enrolled payment method.   |
| `amount`             | integer (cents)     |    yes   |                                              |
| `currency`           | string              |    yes   |                                              |
| `orderId`            | string              |    no    | Merchant reference.                          |
| `effectDate`         | string              |    no    | ISO 8601, first installment date.            |
| `rrule`              | string              |    no    | RFC 5545 iCalendar (`RRULE:FREQ=MONTHLY;INTERVAL=1`). Stored as-is, **not consumed by an internal engine**. |
| `metadata`           | `map[string]string` |    no    |                                              |

**Response**:

```json
{ "status": "SUCCESS", "answer": { "subscriptionId": "<uuid>" } }
```

### Transaction/Get

Returns the state of a transaction indexed by UUID. An unknown UUID
produces a `200 OK` with `status=ERROR` in the envelope
(`PAYSIM_UUID_UNKNOWN`) — following the PayZen contract of never
using HTTP errors for business failures.

**Request** — `TransactionGetRequest`:

| Field  | Type   | Required |
| ------ | ------ | :------: |
| `uuid` | string |    yes   |

**Response** — `TransactionGetAnswer`:

| Field               | Type              | Notes                                       |
| ------------------- | ----------------- | ------------------------------------------- |
| `uuid`              | string            |                                             |
| `orderId`           | string            |                                             |
| `amount`            | integer (cents)   |                                             |
| `currency`          | string            |                                             |
| `orderStatus`       | domain state      | `initiated` / `authorized` / `captured` / … |
| `paymentMethodType` | string            | Optional (`CARDS`, `IP_WIRE`, …).           |
| `creationDate`      | string            | ISO 8601 UTC (RFC 3339).                    |
| `lastUpdateDate`    | string            | Idem.                                       |
| `customer`          | `Customer`        | If set at creation.                         |
| `metadata`          | `map[string]string` |                                           |

### Subscription/Get

**Request** — `SubscriptionGetRequest`:

| Field                | Type   | Required | Notes                                     |
| -------------------- | ------ | :------: | ----------------------------------------- |
| `subscriptionId`     | string |    yes   |                                           |
| `paymentMethodToken` | string |   yes    | Must match the subscription's payment method. A mismatched pair answers like an unknown subscription. |

**Response** — `SubscriptionGetAnswer`:

| Field                | Type                | Notes                                    |
| -------------------- | ------------------- | ---------------------------------------- |
| `subscriptionId`     | string              |                                          |
| `orderId`            | string              |                                          |
| `amount`             | integer             |                                          |
| `currency`           | string              |                                          |
| `effectDate`         | string              |                                          |
| `rrule`              | string              |                                          |
| `paymentMethodToken` | string              |                                          |
| `creationDate`       | string              | ISO 8601.                                |
| `metadata`           | `map[string]string` |                                          |

## Paysim control endpoints (not PayZen)

These endpoints do **not exist** on real PayZen. They stand in for
what the merchant-side SmartForm and the PSP backend would trigger
between them. Kept under a `/paysim/simulate/` prefix so the URL
namespace makes the intent obvious.

Bearer authentication if `PAYSIM_API_TOKEN` is configured — see
[install.md](../install.md).

### POST /paysim/simulate/browserReturn

Simulates the browser POST that follows a completed payment form.
Paysim sends a signed `kr-answer` to `ReturnURL`.

**Request** — `BrowserReturnRequest`:

| Field               | Type               | Required | Notes                                                  |
| ------------------- | ------------------ | :------: | ------------------------------------------------------ |
| `formToken`         | string             |    yes   |                                                        |
| `returnUrl`         | string             |    no    | Overrides the one stored at CreatePayment.             |
| `outcome`           | string             |    yes   | See [outcomes](#outcomes).                             |
| `paymentMethodType` | string             |    no    | Default `CARDS`.                                       |
| `cardBrand`         | string             |    no    | Default `VISA`.                                        |
| `wallet`            | string             |    no    | `APPLE_PAY`, `GOOGLEPAY`, empty.                       |
| `threeDSStatus`     | string             |    no    | `SUCCESS` (default) / `CHALLENGE` / `FAILURE` / `NOT_ENROLLED`. |
| `errorCode`         | string             |    no    | For `outcome=UNPAID`.                                  |
| `errorMessage`      | string             |    no    |                                                        |
| `chaos`             | `WebhookChaos`     |    no    | See [chaos values](#chaos-values-magic-values).        |
| `deliveryDelayMs`   | integer            |    no    | Delay webhook delivery (milliseconds).                 |

**Response** — `BrowserReturnResponse`:
```json
{ "status": "SUCCESS", "deliveryId": "<uuid>", "krHash": "<hex>" }
```

### POST /paysim/simulate/ipn

Same as `browserReturn`, but the resulting POST targets
`notificationUrl` instead of `returnUrl` — the merchant-side
server-to-server webhook.

## kr-answer structure

Full payload sent in the `kr-answer` POST field to the merchant
(browser return or IPN). Signed via
[`kr-hash`](#kr-hash-signature).

```
KrAnswer
├── shopId              string (optional)
├── orderCycle          string
├── orderStatus         string       "PAID" | "UNPAID" | …
├── serverDate          string       ISO 8601
├── serverUrl           string (optional)
├── applicationVersion  string (optional)
├── mode                string       "TEST" (Paysim never emits "PRODUCTION")
├── orderDetails
│   ├── orderTotalAmount     integer
│   ├── orderCurrency        string
│   ├── mode                 string
│   ├── orderId              string
│   ├── orderEffectiveAmount integer
│   └── _type                "V4/OrderDetails"
├── customer            Customer (optional)
├── transactions[]      one entry per payment (single today)
│   ├── uuid                 string
│   ├── amount               integer
│   ├── currency             string
│   ├── paymentMethodType    string
│   ├── paymentMethodToken   string (present when an alias was created or replayed)
│   ├── paymentMethodTokenStatus  "ACTIVE" | "CANCELLED" (alongside the token)
│   ├── status               "PAID" | "UNPAID"
│   ├── detailedStatus       "AUTHORISED" | "CAPTURED" | "REFUSED" | …
│   ├── operationType        "DEBIT" | "CREDIT"
│   ├── creationDate         string
│   ├── errorCode            string (optional)
│   ├── errorMessage         string (optional)
│   ├── metadata             map[string]string (optional)
│   ├── transactionDetails
│   │   ├── mid              string (optional)
│   │   ├── creationContext  "CHARGE"
│   │   ├── wallet           string (optional)
│   │   ├── cardDetails      (CARDS/CB only, see below)
│   │   ├── threeDSResponse  (see below)
│   │   └── _type            "V4/TransactionDetails"
│   └── _type                "V4/PaymentTransaction"
├── subscriptionId      string (optional)
└── _type               "V4/Payment"
```

`cardDetails`:

```
KrCardDetails
├── pan               string     always masked (e.g. "411111XXXXXX1111")
├── brand             string
├── holderName        string     omitted when not supplied at enrolment
├── productCategory   string     "CREDIT" fallback
├── expiryMonth       integer
├── expiryYear        integer
├── country           string     "FR" fallback
├── issuerName        string     "PAYSIM" fallback
├── effectiveBrand    string
└── _type             "V4/CardDetails"
```

**Every field above is derived from the card actually enrolled**, when
there is one — the `card` object of `CreatePayment`, stored as a payment
method. What Paysim announces is therefore what Paysim holds: masked PAN,
expiry date, holder and issuer attributes all match the stored record.

The values marked *fallback* apply only when the enrolment left them
empty. They are **not** applied on top of a supplied value: enrol a card
with `country: "US"` and `productCategory: "DEBIT"` and the webhook
reports exactly that — which is what makes a foreign card or a debit card
testable.

A one-shot payment with no card ever submitted is the one case where the
whole block is synthetic: there is nothing real to describe, so Paysim
issues a demonstration card built from the brand alone.

`threeDSResponse`:

```
KrThreeDSResponse
├── authenticationResultData
│   ├── status              "SUCCESS" | "FAILURE" | "NOT_ENROLLED" | "UNAVAILABLE"
│   ├── authenticationType  "FRICTIONLESS" | "CHALLENGE" (derived from status)
│   └── _type               "V4/AuthenticationResultData"
└── _type                   "V4/ThreeDSResponse"
```

## Outcomes

Values accepted in `browserReturn` / `ipn` `outcome` field:

| Value        | Domain effect                                | Webhook status | Webhook detailedStatus |
| ------------ | -------------------------------------------- | :------------: | :--------------------: |
| `PAID`       | `Capture()` — funds debited                  | `PAID`         | `CAPTURED`             |
| `AUTHORISED` | `Authorize()` — funds reserved, not debited  | `PAID`         | `AUTHORISED`           |
| `UNPAID`     | `Decline(reason)` — bank refusal             | `UNPAID`       | `REFUSED`              |
| `EXPIRED`    | `Expire()` — timeout                         | `UNPAID`       | `EXPIRED`              |
| `ABANDONED`  | Mapped to `Expire()` (no domain state)       | `UNPAID`       | `ABANDONED`            |

### Empty customer fields serialise to `null`

PayZen exposes the customer blocks **in full**, fields included, valued at `null` when
empty — not as an empty object:

```json
"customer": {
  "email": null,
  "reference": null,
  "billingDetails": { "address": null, "firstName": null, "city": null, … },
  "shippingDetails": { "address": null, "category": null, … },
  "extraDetails": { "ipAddress": null, … }
}
```

Paysim matches this. Two gaps were closed at once:

- **Missing key** — the structural one. `Object.keys()`, iteration, `in` and a
  non-optional type all diverge when a key is absent. That is what produces a
  "worked in test, broke in prod".
- **`""` instead of `null`** — the quieter one, but real: `firstName ?? "N/A"` yields
  `"N/A"` against PayZen and `""` here, since the empty string is not nullish.

Fields stay plain strings in the model — no pointers, no dereferencing — and the
conversion happens on the way out only. Decoding is unaffected: `null` maps to the zero
value, as it always did.

## The alias owns its customer

A `paymentMethodToken` — an *alias*, in PayZen's own wording — belongs to a
**customer**, never to an order. That relationship has a consequence Paysim now
reproduces:

> During a payment by alias, the `customer.reference`, `customer.email` and
> `customer.billingDetails` attributes sent in the request are **ignored**, and the
> values stored with the alias are used.

```
Enrolment   : token T, customer.reference = "client-A"
Charge by T : customer.reference = "client-B"   ← merchant-side mistake

PayZen answers : "client-A"    (the alias wins, the bug stays invisible)
Paysim answers : "client-A"    (same — since v0.5.4)
```

Before v0.5.4 Paysim echoed back whatever the request contained. That made it *more
logical* than the real gateway — and therefore misleading: a wrong customer reference
passed validation against Paysim, then silently drifted in production. Reproducing the
protocol as it is, quirks included, is invariant 3.

**`shippingDetails` and `extraDetails` are not overridden.** A delivery address belongs
to the order — the same card ships to different places — and the browser context belongs
to the session. PayZen does not claim to replace them either.

Aliases enrolled before v0.5.4 carry no customer: the charge then falls back to the
request's, for lack of anything better.

## Decline reasons

A merchant does not treat every decline alike: insufficient funds is retried a few days
later, an opposition means asking for another card right away. Without a reason in the
response, that retry logic can neither be written nor tested — it ships blind, and gets
discovered through a customer suspended by mistake.

Declined transactions carry the reason in `detailedErrorCode`, alongside a human-readable
`detailedErrorMessage`:

```json
"transactions": [{
  "status": "UNPAID",
  "detailedStatus": "REFUSED",
  "errorCode": "PAYSIM_REFUSED",
  "detailedErrorCode": "51",
  "detailedErrorMessage": "provision insuffisante"
}]
```

| Code | Meaning                     | Typical merchant response      |
| :--: | --------------------------- | ------------------------------ |
| `51` | Insufficient funds          | retry in a few days            |
| `43` | Stolen card, opposition     | ask for another payment method |
| `91` | Issuer unavailable          | retry shortly                  |
| `05` | Do not honour (generic)     | no signal either way           |
| `57` | Not permitted to cardholder | ask for another payment method |

**These are ISO 8583 authorization return codes**, not Paysim values: they are what the
acquirer sends back and what PayZen relays as-is. Reproducing them verbatim is the only
way a mapping written against Paysim stays valid in production. Note the contrast with
`errorCode`, which *is* prefixed `PAYSIM_` — that one is a PSP-level code, and inventing
a PayZen-looking value there would be passing ourselves off as the real thing.

**Paysim never interprets these codes and exposes no `retryable` flag.** Deciding that a
`51` is worth retrying and a `43` is not is a merchant policy, not protocol data. Adding
that verdict would hand you a semantic the real gateway does not provide — and your
retry logic would then be written against us rather than against PayZen.

Two levers set the reason: the **magic amount** on the checkout path, the **test PAN** on
the recurring one, where the amount is fixed by the subscription. See
[testing-cards.md](../testing-cards.md).

### The same reason goes by two names

`detailedErrorCode` is the **PayZen** name, the one in `kr-answer`. Paysim's own control
API exposes the same data flat, under two different fields:

| Where you read it                             | Code                | Message                  |
| --------------------------------------------- | ------------------- | ------------------------ |
| PayZen `kr-answer` (`transactions[0]`)         | `detailedErrorCode` | `detailedErrorMessage`   |
| Control API (`/paysim/api/v1/payments`)        | `declineCode`       | `declineMessage`         |

This is not a rewrite of the protocol: the control API is not PayZen, and giving it a
PayZen field name would suggest it mimics that format. But coding from this page without
knowing the second pair means looking for a field that does not exist — and a missing
reason raises nothing, it just stays empty.

Both pairs appear everywhere a decline can be read: in a payment summary, in its detail,
and **in the creation response itself** when the outcome is immediate (one-click replay,
autoplay). No extra read is needed to learn the reason.

A decline with no bank reason leaves both fields empty — an abandon, an expiry, a revoked
payment method: no issuer declined there, and inventing an authorization code would
announce a banking decision that never happened.

## Chaos values (magic values)

Paysim ships two categories of built-in behaviour tweaks — cf.
[testing-cards.md](../testing-cards.md).

**Amount-based (magic amounts)** — trigger at simulate/charge time:

| Amount ending in | Effect                                                          |
| ---------------- | --------------------------------------------------------------- |
| `01` (cents)     | Force `UNPAID` regardless of requested outcome.                 |
| `03` (cents)     | 30-second latency injected on `CreatePayment` response (timeout test). |

**PAN-based (magic PANs)** — Luhn-valid test PANs reserved for
systematic declines:

| Brand              | PAN                |
| ------------------ | ------------------ |
| Visa               | `4000000000000002` |
| Mastercard         | `5105105105105100` |
| Mastercard series 2| `2223000000000007` |
| American Express   | `378282000000008`  |

**Struct-based (`chaos` object in simulate)** — targeted per-request
webhook chaos:

| Field                | Type    | Effect                                                          |
| -------------------- | ------- | --------------------------------------------------------------- |
| `duplicate`          | boolean | Enqueue the webhook twice (idempotency test).                   |
| `badSignature`       | boolean | Ship a broken `kr-hash` — merchant should reject.               |
| `raceBeforeResponse` | boolean | Delay HTTP simulate response 500 ms — webhook fires first.      |

Plus `deliveryDelayMs` (integer, delivery lag in ms).

## Error codes

Prefix `PAYSIM_*` so they can't be confused with real PayZen codes
(`INT_010`, `PSP_010`, `ACQ_010`, …). Returned in `APIError.errorCode`.

| Code                             | Meaning                                             |
| -------------------------------- | --------------------------------------------------- |
| `PAYSIM_INVALID_REQUEST`         | Bad JSON, missing required field, ill-formed input. |
| `PAYSIM_INVALID_AMOUNT`          | Zero, negative or overflow amount.                  |
| `PAYSIM_INVALID_CURRENCY`        | Not ISO 4217 uppercase 3-letters.                   |
| `PAYSIM_INVALID_PAYMENT`         | Domain invariant violated (invalid transition).     |
| `PAYSIM_UUID_UNKNOWN`            | `Transaction/Get` on unknown UUID.                  |
| `PAYSIM_TOKEN_UNKNOWN`           | `UpdatePayment` on unknown formToken.               |
| `PAYSIM_SUBSCRIPTION_UNKNOWN`    | `Subscription/Get` on unknown id.                   |
| `PAYSIM_STORE_FAILURE`           | Backing store error (SQLite disk full, corruption). |
| `PAYSIM_PAYMENT_METHOD_UNKNOWN`  | One-click charge on unknown `paymentMethodToken`.   |
| `PAYSIM_EXPIRED_CARD`            | Stored payment method past its expiry date.         |
| `PAYSIM_REVOKED_CARD`            | Stored payment method revoked via generic API.      |

## kr-hash signature

Reproduces the PayZen contract byte-for-byte — validated against the
[official Java SDK Lyra](https://github.com/lyra/rest-api-server-java-sdk).

- **Algorithm**: HMAC-SHA-256, lowercase hex encoding (not base64).
- **Message**: raw content of the `kr-answer` field, exact JSON
  string, byte-for-byte.
- **Key**: the HMAC key of the shop (`PAYSIM_PAYZEN_HMAC_KEY` on the
  server, its counterpart on the merchant side).
- **Additional POST fields alongside `kr-answer`**:
  - `kr-hash`: the signature.
  - `kr-hash-algorithm`: `sha256_hmac`.
  - `kr-hash-key`: `sha256_hmac`.
  - `kr-answer-type`: `V4/Payment`.
- **Verification**: constant-time (`hmac.Equal`), never `==`.

**Validation vector** (extracted from the SDK Java Lyra):

- Key: `ktM7bSeTJpclvpm4eEE9N0LIyoxUvsQ9AAYbQI1xQx7Qh`
- Message: empty string `""`
- HMAC-SHA-256 hex: `a95c2b13d50d57858ff38e7abd76c39d644fd5d1cfdcc360e4c61f2fc48d4a5e`

Historical PHP OSS libs (nursit, thelia) concatenate the key to the
message; Paysim follows the official Lyra SDK behaviour instead —
standard HMAC(message, key), no concatenation.

## Related

- [testing-cards.md](../testing-cards.md) — decline levers and magic PANs.
- [subscriptions.md](../subscriptions.md) — subscription lifecycle.
- [scenarios.md](../scenarios.md) — YAML DSL for integration tests.
- [install.md](../install.md) — deployment and configuration.
