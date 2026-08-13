> [🇬🇧 English](SECURITY.md) · [🇫🇷 Français](SECURITY.fr.md)

# Security

## Reporting a vulnerability

**Do not open a public issue for a security flaw.**

Use the **Report a vulnerability** button in the repository's Security
tab (GitHub Private Vulnerability Reporting). The report stays private
until a fix ships.

No contact address is published, and there is no alternative channel.

## Supported versions

Paysim is a preview release. Only the latest published tag is fixed;
there is no backport to earlier tags.

## Scope

Paysim is a payment provider simulator built for development and
automated testing. It is deliberately permissive, and that is a design
decision rather than a defect:

- the provider routes require Basic Auth credentials but **never verify
  them** — any non-empty pair is accepted, since a simulator has no
  merchant account to authenticate against;
- the control API is protected by a Bearer token only when
  `PAYSIM_API_TOKEN` is set, and it is unset by default. That token
  covers the control API alone: the provider routes keep accepting any
  Basic Auth pair, so payments can still be created without it;
- the web UI has no login. Setting `PAYSIM_API_TOKEN` does not take it
  down — the page is still served, its API calls simply answer 401. Put
  a basic auth on the ingress if the UI must not be reachable;
- the HMAC keys and REST passwords shown in the documentation, examples
  and demo scripts are public demonstration values.

The following are therefore **not** vulnerabilities:

- the absence of authentication on the control API or the web UI in the
  default configuration;
- the demonstration credentials and keys published in the documentation;
- data exposure on an instance reachable from a public network — that
  deployment is unsupported, see the warning at the top of the README;
- the absence of transport encryption on a plaintext listener;
- card numbers held in cleartext. This is documented and deliberate:
  Paysim simulates a PSP, and a real card number has no business being
  in it;
- denial of service or resource exhaustion. Nothing is authenticated by
  default, so anyone who can reach the instance can already fill it;
- the invalid signatures, error responses and malformed webhooks Paysim
  produces on demand: those are features. A fidelity gap against the
  simulated provider's protocol is a functional bug — open a public
  issue for it;
- missing security headers, missing rate limiting, or weak TLS settings —
  all of which belong to a deployment shape this project does not
  support;
- automated scanner output with no working reproduction.

The following **are** in scope — defects that let Paysim cause harm
beyond its own perimeter:

- arbitrary code execution on the host, or container escape;
- path traversal, reads or writes outside the expected directories;
- a vulnerable dependency actually reachable from Paysim's own code.

Note that outbound requests to user-supplied callback URLs are the
product's function, not a defect: making a webhook land wherever the
caller asks is what Paysim exists for.

## Handling

A report must carry a reproduction against a default configuration:
which version, which endpoints, and what an attacker obtains that the
sections above do not already grant. A report without one is closed.

The project is maintained on a voluntary basis, with no committed
turnaround. Reports are handled on a best-effort basis, worst first.
There is no bug bounty and no service level agreement.
