> [🇬🇧 English](subscriptions.md) · [🇫🇷 Français](subscriptions.fr.md)

# Abonnements

> **Exemples shell** : les blocs `curl` ci-dessous supposent Git Bash
> sous Windows ou un shell POSIX. Pour PowerShell natif, utiliser
> `Invoke-RestMethod` avec les arguments équivalents (`-Method`,
> `-Uri`, `-Body`, `-ContentType`).

Paysim simule deux mécaniques de paiement récurrent supportées par les
fournisseurs qu'il émule (PayZen aujourd'hui, Stripe à venir) :

- **Token pattern** : le marchand orchestre la récurrence et déclenche
  chaque échéance lui-même. Documenté dans
  [testing-cards.fr.md](testing-cards.fr.md).
- **Abonnements natifs (PSP-driven)** : le marchand déclare l'échéancier
  (`rrule`, `effectDate`) à la création, et le PSP est censé déclencher
  chaque prélèvement de lui-même. Cette page couvre ce second cas.

## Ce que fait Paysim — et ce qu'il ne fait pas

Un vrai PSP fait tourner un **moteur de facturation en arrière-plan**
qui déclenche chaque occurrence `rrule` à la date prévue, ponctionne
le moyen de paiement stocké et notifie le marchand. Paysim est un
simulateur — un scheduler caché nuirait à la reproductibilité d'une
suite de tests.

Choix de conception Paysim : **aucun scheduler en fond**. À la place,
chaque échéance est déclenchée **explicitement** via un endpoint de
contrôle — vous appelez
`POST /paysim/api/v1/subscriptions/{id}/trigger-billing` quand vous
voulez que le prochain prélèvement ait lieu. Ça rend les runs CI
déterministes (pas de dépendance à l'horloge murale) et l'écriture
d'un scénario étape par étape triviale.

Les champs `rrule` et `effectDate` sont **stockés et renvoyés** tels
quels (contrat 3 : reproduire le protocole tel qu'il est), mais jamais
consommés par un moteur interne.

## Cycle de vie

1. **Enrôler un moyen de paiement** — voir le token pattern dans
   [testing-cards.fr.md](testing-cards.fr.md#pan-magique--refus-sur-tout-paiement).
   L'abonnement a besoin d'un `paymentMethodToken` pour prélever.
2. **Créer l'abonnement** — déclarer amount, currency, order id,
   effect date, rrule, metadata. Paysim assigne un `subscriptionId`.
3. **Déclencher chaque échéance** — un `trigger-billing` par période.
   Paysim crée une `Transaction` complète, applique les vérifications
   du moyen de paiement (révoqué / expiré / PAN magique / montant
   magique), retourne le `state` résultant (captured ou declined).
   Le lien Transaction ↔ Subscription est stocké dans
   `Transaction.Metadata["subscriptionId"]` — pas de table dédiée.
4. **Annuler** au choix du marchand (ou opt-out client).
   `cancelled: true` sur l'abonnement, les `trigger-billing` suivants
   remontent `400`.

## Endpoints API

Tous sous `/paysim/api/v1/subscriptions`. Sélection du provider via
le champ `provider` dans le body JSON (défaut `payzen` ; log Debug
émis sur le fallback).

| Méthode | Chemin                            | Rôle                                     |
| ------- | --------------------------------- | ---------------------------------------- |
| POST    | `/`                               | Crée — retourne `{id, cancelled, …}`     |
| GET     | `/{id}`                           | Lit une entrée                           |
| GET     | `/`                               | Liste (filtre par provider en query)     |
| POST    | `/{id}/trigger-billing`           | Déclenche l'échéance suivante            |
| POST    | `/{id}/cancel`                    | Annule (idempotent, 204 sur id inconnu)  |

## Conditions de refus au `trigger-billing`

Mêmes règles que `charge_token` — le même helper `decideReplayOutcome`
tourne côté serveur, donc les quatre leviers documentés dans
[testing-cards.fr.md](testing-cards.fr.md#les-quatre-leviers)
s'appliquent identiquement :

1. Moyen de paiement révoqué (via `/payment-methods/{token}/revoke`).
2. Carte expirée (`expiryYear`/`expiryMonth` avant le mois courant).
3. PAN magique (un des quatre PANs de test réservés).
4. Montant magique (deux derniers chiffres `01`).

Un abonnement annulé court-circuite toute la chaîne : `400` avec
`abonnement annule`.

## Scénario type

```yaml
name: subscription-monthly
description: Plan mensuel — enrôlement, deux échéances, annulation.
steps:
  - action: create_payment
    provider: payzen
    amount: 100
    currency: EUR
    order_id: INIT
    card:
      pan: "4111111111111111"
      expiry_month: 12
      expiry_year: 2028
  - action: create_subscription
    amount: 2990
    currency: EUR
    order_id: SUB-42
    effect_date: "2026-09-01"
    rrule: "RRULE:FREQ=MONTHLY;INTERVAL=1"
    metadata:
      plan: pro
  - action: trigger_billing
  - action: assert_state
    state: captured
  - action: trigger_billing
  - action: assert_state
    state: captured
  - action: cancel_subscription
  - action: assert_subscription
    cancelled: true
```

Équivalent en HTTP brut :

```bash
# 1. Enrôler un moyen de paiement (retourne {uuid, paymentMethodToken})
curl -X POST http://paysim:8080/paysim/api/v1/payments \
  -H 'Content-Type: application/json' \
  -d '{
    "provider":"payzen","amount":100,"currency":"EUR","orderId":"INIT",
    "card":{"pan":"4111111111111111","expiryMonth":12,"expiryYear":2028}
  }'

# 2. Créer l'abonnement (retourne {id, ...})
curl -X POST http://paysim:8080/paysim/api/v1/subscriptions \
  -H 'Content-Type: application/json' \
  -d '{
    "paymentMethodToken":"<TOKEN>",
    "amount":2990,"currency":"EUR","orderId":"SUB-42",
    "effectDate":"2026-09-01T00:00:00Z",
    "rrule":"RRULE:FREQ=MONTHLY;INTERVAL=1"
  }'

# 3. Déclencher l'échéance suivante (retourne {paymentUuid, state})
curl -X POST http://paysim:8080/paysim/api/v1/subscriptions/<ID>/trigger-billing

# 4. Annuler (204)
curl -X POST http://paysim:8080/paysim/api/v1/subscriptions/<ID>/cancel
```

## Cross-provider

Le champ `provider` sélectionne l'adaptateur — `payzen` aujourd'hui,
`stripe` à venir. En attendant, toute requête sans
`provider` retombe sur `payzen` par défaut. Le passer explicitement
reste valide et prépare la portabilité :

```bash
curl -X POST http://paysim:8080/paysim/api/v1/subscriptions \
  -d '{"provider":"payzen","paymentMethodToken":"…","amount":990,"currency":"EUR"}'
```

## Voir aussi

- [testing-cards.fr.md](testing-cards.fr.md) — les quatre leviers de
  refus et le token pattern (récurrent one-shot).
- [ROADMAP.md](../ROADMAP.md) — la phase 4 (4.4.6) couvre les
  abonnements.
