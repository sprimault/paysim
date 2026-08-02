> [🇬🇧 English](README.md) · [🇫🇷 Français](README.fr.md)

# Paysim

> Faux prestataire de paiement qui provoque les échecs qu'une sandbox refuse de reproduire.

## Ce qu'est Paysim

Paysim est un faux prestataire de paiement auto-hébergé. Il reproduit
le protocole de fil d'un vrai PSP (PayZen aujourd'hui, Stripe
ensuite) assez fidèlement pour qu'une intégration marchande réelle
lui parle sans modification — mêmes endpoints, mêmes signatures,
mêmes formes de webhooks.

## Pourquoi Paysim existe

Les sandbox réussissent toujours. Les vrais paiements non. Les tests
d'intégration qui ont le plus de valeur ne sont pas « est-ce que la
capture fonctionne » — la sandbox couvre déjà ça — mais « est-ce que
mon marchand survit quand la capture renvoie 500 en cours, quand le
webhook arrive avant la réponse HTTP, quand le même webhook est
livré deux fois, quand le 3DS est abandonné, quand le client est
déclaré impayé trois jours plus tard ». Paysim est ce qu'on utilise
pour provoquer ces cas à la demande, en dev et en CI.

## Ce qu'on peut faire avec

- Faire courir le webhook contre la réponse HTTP (la seule course qu'une sandbox ne donne jamais).
- Réordonner ou dupliquer la livraison des webhooks.
- Injecter latence, erreurs 5xx, signatures invalides.
- Refuser avec des montants magiques, des PAN magiques, des cartes expirées, des tokens révoqués.
- Rejouer n'importe quel webhook depuis l'UI ou l'API.

## Fournisseurs

| Fournisseur | API | Couverture |
|---|---|---|
| PayZen / Lyra Collect | REST V4 | Complète — voir [`docs/providers/payzen.fr.md`](docs/providers/payzen.fr.md) |
| Stripe | — | Plus tard |

## Démarrage rapide (Docker Compose)

**Linux / macOS / Git Bash :**

```bash
docker compose -f deploy/compose.yml up -d
bash examples/seed-paysim.sh
# http://localhost:30880/

# Re-lance pour repartir sur des paiements propres (le seed n'est pas idempotent) :
bash examples/seed-paysim.sh --purge
```

**Windows PowerShell :**

```powershell
docker compose -f deploy/compose.yml up -d
.\examples\seed-paysim.ps1 -Purge
# http://localhost:30880/

# Re-lance pour repartir sur des paiements propres :
.\examples\seed-paysim.ps1 -Purge
```

Surcharger `PAYSIM_HOST_PORT` (et passer le même à `PAYSIM_URL` sur
la ligne du seed) si `30880` est déjà pris sur la machine.

Reconstruire après un changement de code — force le rebuild de
l'image et la recréation du conteneur :

```bash
docker compose -f deploy/compose.yml up -d --build --force-recreate
```

Si erreur `No such container`, Docker Compose a perdu la trace du
conteneur (`docker rm` manuel hors compose, etc.). Reset d'abord :

```bash
docker compose -f deploy/compose.yml down --remove-orphans
docker compose -f deploy/compose.yml up -d
```

Le script de seed peuple l'UI avec un jeu de données varié —
paiements, abonnements, moyens de paiement dans tous les états
visuels (captured, refusé, actif, révoqué, expiré). Utile pour une
première prise en main.

## Installation complète

[`docs/install.fr.md`](docs/install.fr.md) couvre Docker Compose,
Kubernetes (NodePort ou Ingress), la **matrice des deux URL** (la
section qu'on cherche vraiment), et la persistance SQLite optionnelle.

## Quatre leviers de refus

Montant magique se terminant par `01`, quatre PAN magiques
canoniques, expiration de carte, révocation de token. Détails dans
[`docs/testing-cards.fr.md`](docs/testing-cards.fr.md).

## Scénarios (YAML)

Rejouer un flux de paiement en CI sans écrire de curl à la main :

```yaml
- action: create_payment
  amount: 4990
  currency: EUR
  register: true
  card: { pan: "4111111111111111", expiry_month: 12, expiry_year: 2028 }
- action: assert_state
  state: captured
```

11 actions supportées. Voir [`docs/scenarios.fr.md`](docs/scenarios.fr.md).

## Exemple d'intégration PHP

Basculer une intégration PayZen vers Paysim, c'est un changement d'URL :

```php
$client = new PayzenClient([
    'endpoint'  => 'http://localhost:30880',  // au lieu de https://api.payzen.eu
    'username'  => '00000000',                 // n'importe quelle valeur non vide
    'password'  => 'testpassword_XXXX',
    'hmac_key'  => 'dev-hmac-key',             // correspond à PAYSIM_PAYZEN_HMAC_KEY
]);
$response = $client->post('/api-payment/V4/Charge/CreatePayment', [...]);
```

Marchand complet avec vérification du webhook :
[`examples/php`](examples/php/README.fr.md).

Ou directement avec `curl` — même body qu'un client PayZen REST V4 :

```bash
curl -X POST http://localhost:30880/api-payment/V4/Charge/CreatePayment \
  -u 00000000:testpassword_XXXX \
  -H 'Content-Type: application/json' \
  -d '{"amount":4990,"currency":"EUR","orderId":"CMD-42","customer":{"email":"a@b.io"}}'
```

## Interface web

SPA React embarquée — paiements, abonnements, moyens de paiement,
webhooks (avec rejeu en un click), tout en temps réel via SSE. Mode
sombre, rechargement automatique quand un nouveau build est déployé,
bouton d'actualisation par vue.

## Statut

Préversion, tag `v0.4.0` (2026-08-02). Le support Stripe et une
sortie publique avec un GIF de démo sont prévus.

## Licence

Apache 2.0 — voir [`LICENSE`](LICENSE).
