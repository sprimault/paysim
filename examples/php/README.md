> [🇬🇧 English](README.md) · [🇫🇷 Français](README.fr.md)

# PHP integration example

This folder demonstrates a full merchant-side payment flow against
Paysim, in plain PHP (no Composer dependency). Three scripts:

- **`scenario.php`** — orchestrates the simulation: calls
  `POST /api-payment/V4/Charge/CreatePayment` to obtain a `formToken`,
  then `POST /paysim/simulate/browserReturn` to trigger a signed
  browser return to `return.php`.
- **`return.php`** — endpoint that receives the browser return POST
  (`kr-answer` + `kr-hash`), **verifies the signature with
  `hash_hmac('sha256', ...)`** and writes the result to a local log.
- **`notification.php`** — the equivalent for the server-to-server IPN,
  with the same verification logic.

This example satisfies the phase-1 completion criterion: a PHP
merchant performs a full payment against Paysim by changing only the
base URL, and verifies the `kr-hash` signature produced by Paysim on
its own side.

## Prerequisites

- PHP 8.1+ with the `curl` and `openssl` extensions (standard).
- The Paysim binary compiled (`make build` at the repo root).

## How to run

In three separate terminals:

### 1. Start Paysim

```bash
export PAYSIM_PUBLIC_URL="http://localhost:8080"
export PAYSIM_CALLBACK_URL="http://localhost:9000"
export PAYSIM_PAYZEN_HMAC_KEY="cle-hmac-de-test"
./paysim
```

### 2. Start the demo PHP server (merchant)

From this folder:

```bash
php -S localhost:9000
```

It will serve `return.php` and `notification.php` at
`http://localhost:9000/`.

### 3. Run the scenario

```bash
php scenario.php
```

The script:

1. Calls Paysim → gets a `formToken`.
2. Triggers a PAID return via `/paysim/simulate/browserReturn`.
3. The PHP server receives the POST on `return.php`, checks `kr-hash`,
   writes to `retours.log`.
4. `scenario.php` prints a summary of the flow.

## Signature verification

The point of the demo: `return.php` recomputes the hash on the
merchant side with the same HMAC key and compares in constant time.

```php
$expected = hash_hmac('sha256', $krAnswer, $hmacKey);
if (!hash_equals($expected, $krHash)) {
    http_response_code(400);
    exit('signature invalide');
}
```

This is the exact logic a production merchant must implement — Paysim
mirrors it faithfully, an integrator can switch between Paysim (test)
and PayZen (production) without touching this code.

## HMAC key

The test key in the scripts (`cle-hmac-de-test`) must match
`PAYSIM_PAYZEN_HMAC_KEY` on the Paysim server side, otherwise
`kr-hash` verification fails every time.
