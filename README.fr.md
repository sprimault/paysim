> [🇬🇧 English](README.md) · [🇫🇷 Français](README.fr.md)

# Paysim

![License](https://img.shields.io/badge/license-Apache%202.0-blue)
![Release](https://img.shields.io/github/v/release/sprimault/paysim)
![Image](https://img.shields.io/badge/image-ghcr.io%2Fsprimault%2Fpaysim-blue?logo=docker)

> Faux prestataire de paiement qui provoque les échecs qu'une sandbox refuse de reproduire.

> [!WARNING]
> Paysim est un simulateur destiné au développement et aux tests
> automatisés. Il ne traite aucun paiement réel, ne contacte aucun
> établissement bancaire et ne doit en aucun cas être déployé en
> production ni exposé sur un réseau public. Les identifiants et clés
> HMAC fournis dans la documentation sont des valeurs de démonstration
> publiques : toute instance accessible depuis Internet est réputée
> ouverte à tous.

![Renvoi d'une livraison de webhook, examen d'un paiement refusé, et détection par l'interface de la coupure du serveur](docs/assets/demo.webp)

<sub>Vingt secondes contre une instance réelle — [film complet (42 s)](docs/assets/demo-complete.webp).</sub>

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

Récupérer le code d'abord (même commande sur les deux plateformes) :

```bash
git clone https://github.com/sprimault/paysim.git
cd paysim
```

Puis (aucun build local nécessaire — `docker compose` tire l'image
prête depuis `ghcr.io/sprimault/paysim:latest`, multi-arch amd64+arm64) :

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
.\examples\seed-paysim.ps1
# http://localhost:30880/

# Re-lance pour repartir sur des paiements propres :
.\examples\seed-paysim.ps1 -Purge
```

Surcharger `PAYSIM_HOST_PORT` (et passer le même à `PAYSIM_URL` sur
la ligne du seed) si `30880` est déjà pris sur la machine.

**Linux / macOS / Git Bash :**

```bash
PAYSIM_HOST_PORT=30890 docker compose -f deploy/compose.yml up -d
PAYSIM_URL=http://localhost:30890 bash examples/seed-paysim.sh --purge
# http://localhost:30890/
```

**Windows PowerShell :**

```powershell
$env:PAYSIM_HOST_PORT="30890"; docker compose -f deploy/compose.yml up -d
$env:PAYSIM_URL="http://localhost:30890"; .\examples\seed-paysim.ps1 -Purge
# http://localhost:30890/
```

Reconstruire après un changement de code — force le rebuild de
l'image et la recréation du conteneur. Même commande sur les deux
plateformes :

**Linux / macOS / Git Bash :**

```bash
docker compose -f deploy/compose.yml up -d --build --force-recreate
```

**Windows PowerShell :**

```powershell
docker compose -f deploy/compose.yml up -d --build --force-recreate
```

Si erreur `No such container`, Docker Compose a perdu la trace du
conteneur (`docker rm` manuel hors compose, etc.). Reset d'abord :

**Linux / macOS / Git Bash :**

```bash
docker compose -f deploy/compose.yml down --remove-orphans
docker compose -f deploy/compose.yml up -d
```

**Windows PowerShell :**

```powershell
docker compose -f deploy/compose.yml down --remove-orphans
docker compose -f deploy/compose.yml up -d
```

Le script de seed peuple l'UI avec un jeu de données varié —
paiements, abonnements, moyens de paiement dans tous les états
visuels (captured, refusé, actif, révoqué, expiré). Utile pour une
première prise en main.

## Essayer sans cloner

Pour juste voir Paysim tourner 5 minutes sans toucher à git, lancer
l'image directement (aucune donnée de seed, aucune persistance).
Même one-liner sur Linux/macOS/Git Bash et Windows PowerShell :

```bash
docker run --rm -p 30880:8080 -e PAYSIM_PUBLIC_URL=http://localhost:30880 -e PAYSIM_CALLBACK_URL=http://localhost:30880 -e PAYSIM_PAYZEN_HMAC_KEY=dev-hmac-key -e PAYSIM_PAYZEN_REST_PASSWORD=dev-rest-password ghcr.io/sprimault/paysim:latest
```

Puis ouvrir http://localhost:30880/.

Pour la démo complète avec UI peuplée (abonnements, moyens de
paiement), utiliser le démarrage rapide Docker Compose ci-dessus —
il active SQLite et lance le script de seed.

## Installation complète

[`docs/install.fr.md`](docs/install.fr.md) couvre Docker Compose,
Kubernetes (NodePort ou Ingress), la **matrice des deux URL** (la
section qu'on cherche vraiment), et la persistance SQLite optionnelle.

## Quatre leviers de refus

Montants magiques se terminant par `01`, `02` ou `04` — chacun son
motif bancaire — quatre PAN magiques canoniques, expiration de carte,
révocation de token. Détails dans
[`docs/testing-cards.fr.md`](docs/testing-cards.fr.md).

## Scénarios (YAML)

Rejouer un flux de paiement en CI sans écrire de curl à la main.
Les scénarios canoniques sont dans
[`examples/scenarios/`](examples/scenarios/). Lancer un scénario
contre le conteneur :

**Linux / macOS / Git Bash :**

```bash
docker compose -f deploy/compose.yml cp examples/scenarios/one-shot.yml paysim:/tmp/one-shot.yml
docker compose -f deploy/compose.yml exec -e PAYSIM_URL=http://localhost:8080 paysim /paysim run /tmp/one-shot.yml
```

**Windows PowerShell :**

```powershell
docker compose -f deploy/compose.yml cp examples/scenarios/one-shot.yml paysim:/tmp/one-shot.yml
docker compose -f deploy/compose.yml exec -e PAYSIM_URL=http://localhost:8080 paysim /paysim run /tmp/one-shot.yml
```

Un fichier scénario minimal ressemble à :

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
    // Signe les notifications serveur — correspond à PAYSIM_PAYZEN_REST_PASSWORD
    'password'  => 'dev-rest-password',
    // Signe le retour navigateur — correspond à PAYSIM_PAYZEN_HMAC_KEY
    'hmac_key'  => 'dev-hmac-key',
]);
$response = $client->post('/api-payment/V4/Charge/CreatePayment', [...]);
```

Marchand complet avec vérification du webhook :
[`examples/php`](examples/php/README.fr.md).

Ou directement avec `curl` — même body qu'un client PayZen REST V4.

**Linux / macOS / Git Bash :**

```bash
curl -X POST http://localhost:30880/api-payment/V4/Charge/CreatePayment \
  -u 00000000:testpassword_XXXX \
  -H 'Content-Type: application/json' \
  -d '{"amount":4990,"currency":"EUR","orderId":"CMD-42","customer":{"email":"a@b.io"}}'
```

**Windows PowerShell natif** (`curl` y est un alias pour
`Invoke-WebRequest` à syntaxe différente, utiliser `Invoke-RestMethod`) :

```powershell
$cred = New-Object PSCredential('00000000', (ConvertTo-SecureString 'testpassword_XXXX' -AsPlainText -Force))
Invoke-RestMethod -Method Post -Uri http://localhost:30880/api-payment/V4/Charge/CreatePayment `
  -Credential $cred -ContentType 'application/json' `
  -Body '{"amount":4990,"currency":"EUR","orderId":"CMD-42","customer":{"email":"a@b.io"}}'
```

## Interface web

SPA React embarquée servie sur le même port que l'API (par défaut
`http://localhost:30880/`) — paiements, abonnements, moyens de
paiement, webhooks (avec rejeu en un click), tout en temps réel via
SSE. Mode sombre, rechargement automatique quand un nouveau build
est déployé, bouton d'actualisation par vue.

## Images publiées

| Tag      | Contenu                                    | Quand elle bouge          |
| -------- | ------------------------------------------ | ------------------------- |
| `latest` | Dernière version stable                    | à chaque release          |
| `edge`   | État de `master`                           | à chaque fusion de PR     |

```bash
docker pull ghcr.io/sprimault/paysim:latest   # stable
docker pull ghcr.io/sprimault/paysim:edge     # en avance d'une release
```

`edge` existe pour qu'un correctif soit installable sans attendre une version.
Elle est publiée par la CI **après** que le lint, les tests, l'audit et les sept
scénarios canoniques sont passés — jamais depuis une pull request. Comme
`latest`, elle est multi-architecture amd64 et arm64.

Utiliser `edge` en production n'a pas de sens : rien n'y garantit la stabilité
d'une interface entre deux fusions.

## Statut

Préversion, tag `v0.6.6`. Le support Stripe est prévu.

**Comment c'est validé.** Chaque pull request exécute le linter, les tests
unitaires avec le détecteur de concurrence, un audit des dépendances, un
contrôle de dérive des types TypeScript générés depuis les structures Go, et
les sept scénarios canoniques contre un vrai binaire, dans les deux modes
de stockage — mémoire et SQLite. Le workflow est public, ses exécutions
sont dans l'onglet Actions. La signature `kr-hash` est vérifiée contre les
vecteurs de la RFC 4231 et contre un vecteur du SDK Java officiel de Lyra
— aucun des deux n'est produit par notre propre code.

L'essentiel des correctifs vient de l'usage plutôt que de la théorie :
brancher Paysim sur une intégration marchande fait apparaître ce qu'aucun
test unitaire ne montre — un champ qui disparaît en silence au décodage, un
motif de refus qui n'atteint jamais le marchand, un alias qui ne porte pas
son client. Chacun de ces défauts devient ensuite un scénario, pour qu'il ne
revienne pas.

## Retours

Bugs, demandes de features, ou questions : ouvrir une issue sur
https://github.com/sprimault/paysim/issues (français de préférence,
anglais accepté).

Pour envoyer un correctif, [`CONTRIBUTING.fr.md`](CONTRIBUTING.fr.md)
énonce les règles sur lesquelles une pull request est jugée — elles ne se
devinent pas à la lecture du code, et aucune ne se négocie dans une pull
request.

## Licence

Apache 2.0 — voir [`LICENSE`](LICENSE).
