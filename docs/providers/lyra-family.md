> [🇬🇧 English](lyra-family.md) · [🇫🇷 Français](lyra-family.fr.md)

# The Lyra family

PayZen, Systempay, Sogecommerce, Scellius and Lyra Collect are the same
gateway under five brands. Paysim covers them all with a single adapter
and no special configuration: **only the host differs, and the host is
what you point at Paysim.**

## What was verified

This is not inferred from documentation. The production APIs were probed
path by path, in August 2026:

- **24 existing** services and **12 non-existing** ones answer the same
  error code, path for path, across the platform's fourteen hosts. A
  made-up path serves as a control — it answers `INT_901` everywhere,
  proving the router really discriminates.
- **One signature rule throughout**: lowercase hex HMAC-SHA-256 over the
  raw `kr-answer`, with `kr-hash-key` naming the key — HMAC key on the
  browser return, REST password on the server notification.
- The `/api-payment/` prefix is **hardcoded** in the official SDK and
  not configurable: only the host comes from configuration.
- The eleven brand archives of the same official plugin release carry
  the **same verification code**, at the same digest.

## The real hosts

Use these instead of Paysim when you switch back to production. **No
rule lets you derive them** — every brand declares itself.

| Brand | REST API |
|---|---|
| PayZen | `api.payzen.eu` |
| Systempay | `api.systempay.fr` |
| Sogecommerce | `api-sogecommerce.societegenerale.eu` |
| Scellius | `api.scelliuspaiement.labanquepostale.fr` |
| Lyra Collect | `api.lyra.com` |

The trap: `secure.payzen.eu` becomes `paiement.systempay.fr`, the prefix
is `api-` with a hyphen on Sogecommerce, and the JavaScript client is
served by a dedicated domain on PayZen but by the API host itself on
Sogecommerce. An integrator who derives a host from a pattern gets it
wrong.

Do not copy the table from the `.env.example` of the official examples
repository: at the time of this check it carried two unusable entries —
an inversion between the API host and the static host for Brazil, and an
Indian host whose name no longer resolves.

## What is not part of it

**India.** `api.in.lyra.com` does not expose REST V4 but an entirely
different API: `/pg/rest/v1/charge` paths, a `DUE` / `PAID` / `DROPPED`
state vocabulary, a different error envelope, and a webhook declared
when the charge is created rather than in the back office. Exclusion was
verified both ways. It is another provider, not a brand — Paysim does
not simulate it.

**The JavaScript client.** Paysim does not serve the SmartForm: your
page loads it from your brand's host. Worth knowing anyway, two builds
coexist — the Latin American one carries a field the European one does
not. No consequence here, but an integrator cannot assume the file
served is the same from one brand to the next.

## What Paysim does not claim to be

The response envelope announces `applicationVersion: 6.0.0-paysim`,
where the real platform announces its own version. That is deliberate: a
simulator passing itself off as a genuine Lyra server would be a trap,
not a tool.
