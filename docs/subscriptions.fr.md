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

**L'alias doit exister.** Un `paymentMethodToken` inconnu fait échouer la
création — `PAYSIM_PAYMENT_METHOD_UNKNOWN` sur la route du fournisseur,
404 sur l'API de contrôle — plutôt que de rendre un abonnement dont chaque
échéance échouerait ensuite. La route native l'acceptait jusqu'à la
`v0.7.3` ; les deux chemins vérifient désormais la même chose.

La lecture et la liste retournent `billingCount` : le nombre d'échéances
déjà produites par l'abonnement, réussies comme refusées. Il est recompté
à la volée depuis les métadonnées des paiements plutôt que stocké — un
compteur persisté serait à maintenir à chaque échéance et pourrait
diverger du réel, ce qu'un simulateur ne doit précisément pas faire.

### Lister les échéances d'un abonnement

```
GET /paysim/api/v1/payments?subscriptionId={id}
```

Retourne les paiements produits par cet abonnement, dans la même forme
que n'importe quelle liste de paiements. C'est la lecture inverse de
`Transaction.Metadata["subscriptionId"]`, et le filtre est appliqué côté
serveur : le sommaire d'un paiement n'expose pas les métadonnées, un
client ne pourrait donc pas trancher lui-même.

Un identifiant inconnu retourne une liste vide, jamais la liste entière.
Un filtre qui dégénère en « pas de filtre » présenterait tous les
paiements de l'instance comme les échéances de cet abonnement.

## Notification de l'échéance

Chaque `trigger-billing` émet un webhook, qu'elle réussisse ou échoue.
C'est le seul moyen pour le marchand d'apprendre qu'une échéance est
passée : une échéance est déclenchée par un ordonnanceur, jamais par
quelqu'un qui attendrait la réponse HTTP.

Un abonnement ne porte pas d'URL de notification propre — la cible est
donc **`PAYSIM_CALLBACK_URL`**. Sans cette variable, aucune notification
n'est émise et une reprise d'impayé devient intestable de bout en bout.
Un log de niveau `WARN` (`fallback_callback_url`) trace la cible
retenue, pour qu'une livraison inattendue reste explicable.

Le webhook porte le résultat métier dans son `outcome` : `PAID` sur une
échéance honorée, `UNPAID` sur un refus. Un échec de livraison ne remet
pas en cause l'échéance elle-même, qui a bien eu lieu.

Le rejeu one-click (`charge_token`) suit la même règle : `notificationUrl`
si elle est fournie dans la requête, `PAYSIM_CALLBACK_URL` sinon.

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
  # Enrôlement sans débit : l'alias est rendu tout de suite. Avec un
  # montant, il n'apparaîtrait qu'après le paiement joué, et l'étape
  # suivante n'aurait pas de token à utiliser.
  - action: create_payment
    provider: payzen
    amount: 0
    currency: EUR
    order_id: INIT
    form_action: REGISTER
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
#
# amount: 0 — une vérification sans débit rend l'alias immédiatement.
# Avec un montant, l'alias n'existe qu'après le paiement joué : PayZen
# ne le crée qu'une fois l'autorisation acceptée.
#
# Noms de champs en camelCase : c'est l'API HTTP. Le DSL YAML des
# scénarios, lui, est en snake_case.
curl -X POST http://paysim:8080/paysim/api/v1/payments \
  -H 'Content-Type: application/json' \
  -d '{
    "provider":"payzen","amount":0,"currency":"EUR","orderId":"INIT",
    "formAction":"REGISTER",
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
