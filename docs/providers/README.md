> [🇬🇧 English](README.md) · [🇫🇷 Français](README.fr.md)

# Providers

What Paysim simulates, which upstream API version each reference is
written against, and where the simulation stops.

## Simulated today

| Provider | Gateway | API version | Simulated surface | Reference |
| --- | --- | --- | --- | --- |
| PayZen | Lyra | REST V4.0 | 5 endpoints | [`payzen.md`](payzen.md) |
| Systempay | Lyra | REST V4.0 | 5 endpoints | same reference |
| Sogecommerce | Lyra | REST V4.0 | 5 endpoints | same reference |
| Scellius | Lyra | REST V4.0 | 5 endpoints | same reference |
| Lyra Collect | Lyra | REST V4.0 | 5 endpoints | same reference |

Those five are one gateway under five brands, so one adapter and one
reference cover them all — only the host differs, and the host is what
you point at Paysim. [`lyra-family.md`](lyra-family.md) gives the real
production hosts, the traps in deriving them, and what is deliberately
excluded (India runs a different API, not a brand of this one).

## Planned

| Provider | Gateway | Status |
| --- | --- | --- |
| Stripe | Stripe | Next — REST + JSON webhooks, complementary protocol axis |
| Monetico | Crédit Mutuel / CIC | Later — sealed form and redirection |

Neither is simulated today. A request against them gets no response
from Paysim, not a degraded one.

## What "API version" means here

The column names the upstream specification a reference was written
against — for the Lyra family, REST V4.0 as published at
[payzen.io](https://payzen.io/en-EN/rest/V4.0/api/) and
[docs.lyra.com](https://docs.lyra.com/en/rest/V4.0/api/).

It is the version an integrator should compare their SDK against. It is
not a claim that every endpoint of that version exists here.

## Coverage is a subset, and the subset is written down

No reference here says "full API". Each one carries two explicit lists:
the endpoints Paysim simulates, and the endpoints it does not — the
latter answer `404` or an unmodelled error rather than a plausible
success.

That distinction is the point of the tool. A simulator that answers
something reasonable to a call it does not implement teaches an
integration to expect a behaviour production will not have.

## When the upstream API moves

Nothing here is generated from the PSP's specification, and no
automated check compares the two: these references are maintained by
hand, against the code, and the code is the source of truth for what
Paysim actually does.

So a gap can open silently when a provider ships a change. If you find
one — a field that has moved, a value that is no longer accepted, an
endpoint that has appeared — it is a bug worth reporting, with the real
capture that shows it. Signature vectors in particular are never
fabricated here; they come from real captures.

## What Paysim never pretends to be

Responses announce `applicationVersion: 6.0.0-paysim` where the real
platform announces its own. That is deliberate. A simulator passing
itself off as a genuine server would be a trap rather than a tool, and
anything reaching these endpoints should be able to tell it is talking
to a fake.
