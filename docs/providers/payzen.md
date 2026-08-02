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
| `customer`         | `Customer`          |    no    | Buyer info — email + billing details.                       |
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
| `REGISTER`         | Enrollment only (0 amount) — accepted by PayZen, treated as PAYMENT by Paysim today. |

**Paysim specifics**:

- The `card` field is **not part of the real PayZen contract** — in
  production the card data transits via the SmartForm client
  (`kr-payment-form.min.js`), never through the merchant API. Paysim
  accepts it as an integration convenience: attaching a card triggers
  systematic enrollment (independent of `formAction`) and returns a
  `paymentMethodToken` in the webhook. See
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
| `paymentMethodToken` | string |    no    | PayZen requires it in real world; Paysim ignores it (id alone is unique). |

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
├── transactions[]      one entry per payment (single in phase 1)
│   ├── uuid                 string
│   ├── amount               integer
│   ├── currency             string
│   ├── paymentMethodType    string
│   ├── paymentMethodToken   string (present after REGISTER_PAY or replay)
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
├── productCategory   string     "CREDIT" default
├── expiryMonth       integer
├── expiryYear        integer
├── country           string     "FR" default
├── issuerName        string     "PAYSIM" — Paysim marker
├── effectiveBrand    string
└── _type             "V4/CardDetails"
```

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
