> [🇬🇧 English](scenarios.md) · [🇫🇷 Français](scenarios.fr.md)

# Scénarios

Paysim embarque un DSL YAML de scénarios et un runner (`paysim run
scenario.yml`) pour qu'un test d'intégration puisse enchaîner une
séquence d'étapes contre Paysim et asserter l'issue. Même runner en
local, en CI, ou contre un Paysim déployé sur un cluster — seule
`PAYSIM_URL` change.

Les exemples canoniques vivent dans
[examples/scenarios/](../examples/scenarios/) — cinq fichiers courts
qui couvrent one-shot, token pattern et subscription native.

## Comment lancer

```bash
export PAYSIM_URL=http://paysim:8080
paysim run scenarios/one-shot.yml
```

Codes de retour (adaptés à la CI) :

| Code | Sens                                                              |
| :--: | ----------------------------------------------------------------- |
|  0   | Toutes les étapes ont passé.                                      |
|  1   | Assertion échouée (`assert_state`, `assert_webhook`, `assert_subscription`). |
|  2   | Erreur d'exécution (fichier absent, YAML invalide, HTTP down, action inconnue). |

Flag optionnel `--verbose` imprime chaque étape dès qu'elle finit.
`PAYSIM_API_TOKEN` est pris si défini (doit correspondre à la config serveur).

## Format YAML

Chaque scénario a un `name`, une `description` optionnelle et une liste
ordonnée de `steps`. Chaque étape porte un discriminant `action` explicite
et ses propres champs. Choix délibérément verbeux (contre le style
Ansible à clé implicite) — les méta-champs à venir (`id`, `timeout`,
`retry`) restent au même niveau que `action` sans casser la symétrie.

```yaml
name: mon-scenario
description: Ce que ce scénario vérifie.
steps:
  - action: create_payment
    provider: payzen
    amount: 1000
    currency: EUR
    order_id: O-1
  - action: simulate
    status: captured
  - action: assert_state
    state: captured
```

Noms de champs en `snake_case` en YAML, `camelCase` sur le fil (API JSON).

## Référence des actions

Onze actions qui couvrent les trois patterns de paiement.

### Paiements one-shot

| Action           | Rôle                                                                    |
| ---------------- | ----------------------------------------------------------------------- |
| `create_payment` | Crée un paiement. `card`, `form_action`, `notification_url` optionnels. |
| `simulate`       | Fait avancer le paiement via l'endpoint de simulation navigateur.       |
| `assert_state`   | Assert que le paiement courant est dans l'état demandé.                 |
| `assert_webhook` | Compte les webhooks livrés depuis le début du scénario (`status` optionnel).|

### Récurrence par token

| Action         | Rôle                                                                             |
| -------------- | -------------------------------------------------------------------------------- |
| `charge_token` | Déclenche un prélèvement récurrent one-click via le dernier `paymentMethodToken` enrôlé. `token` optionnel (défaut `currentToken`). |

### Abonnements natifs (PSP-driven)

| Action                | Rôle                                                                    |
| --------------------- | ----------------------------------------------------------------------- |
| `create_subscription` | Enregistre un abonnement contre le dernier moyen de paiement enrôlé.    |
| `trigger_billing`     | Déclenche l'échéance suivante. `subscription_id` optionnel.             |
| `assert_subscription` | Vérifie l'existence (et optionnellement `cancelled: true/false`).       |
| `cancel_subscription` | Annule. Idempotent.                                                     |

### Utilitaires

| Action  | Rôle                                                          |
| ------- | ------------------------------------------------------------- |
| `wait`  | Suspend l'exécution pendant `duration` (`"500ms"`, `"2s"`).   |
| `inject`| Empile un mode chaos consommé par le **prochain** `simulate`. |

Modes `inject` reconnus (one-shot — consommé par le prochain `simulate`,
puis remis à zéro) :

| Mode              | Effet sur le webhook déclenché par le prochain `simulate`             |
| ----------------- | --------------------------------------------------------------------- |
| `duplicate`       | Webhook enqueue deux fois (test idempotence côté marchand).           |
| `bad-signature`   | `kr-hash` altéré — le marchand qui vérifie la signature doit refuser. |
| `race`            | Réponse HTTP simulate retardée 500 ms ; le webhook part en premier.   |
| `delay=NNN`       | Retarde la livraison du webhook de NNN millisecondes (compose avec un second `simulate` pour tester le out-of-order). |

Chaos persistant : réinjecter avant chaque `simulate` concerné. Voir
`examples/scenarios/chaos-duplicate.yml`.

## État implicite — un paiement / un token / une subscription à la fois

Le runner mémorise trois références au fil du scénario, permettant à la
plupart des étapes d'omettre leur `id`/`token` :

- `currentUUID` — mis à jour à chaque `create_payment`, `charge_token`,
  `trigger_billing`. Consommé par `assert_state` et `assert_webhook`.
- `currentToken` — mis à jour dès qu'un `create_payment` retourne un
  `paymentMethodToken` (dès qu'une `card` est fournie). Consommé par
  `charge_token` et `create_subscription` si leur champ `token` est vide.
- `currentSubID` — mis à jour à chaque `create_subscription`. Consommé
  par `trigger_billing`, `assert_subscription`, `cancel_subscription`
  si leur champ `subscription_id` est vide.

Pour un scénario qui jongle avec plusieurs paiements/tokens/subscriptions,
passer les ids explicitement.

## Cross-provider

Le champ `provider` sur `create_payment`, `charge_token` et
`create_subscription` sélectionne l'adaptateur — `payzen` par défaut.
Quand Stripe arrivera en phase 5 (`provider: stripe`), les scénarios
écrits aujourd'hui continueront de fonctionner sans changement ; seuls
les nouveaux scénarios devront opter explicitement pour le nouveau
provider.

Détails dans [testing-cards.fr.md](testing-cards.fr.md#multi-provider)
et [subscriptions.fr.md](subscriptions.fr.md#cross-provider).

## Scénarios canoniques

Voir [examples/scenarios/](../examples/scenarios/) :

- `one-shot.yml` — paiement nominal, capture réussie.
- `one-shot-declined.yml` — refus via montant magique (`1001`).
- `recurring-token.yml` — récurrence orchestrée marchand avec moyen
  de paiement enregistré.
- `subscription.yml` — subscription PSP-driven avec deux échéances et
  annulation.
- `subscription-with-decline.yml` — subscription dont l'échéance
  échoue à cause d'un PAN magique.
- `chaos-duplicate.yml` — injection du mode `duplicate`, assertion
  que le webhook arrive deux fois (test d'idempotence marchand).

## Voir aussi

- [testing-cards.fr.md](testing-cards.fr.md) — les quatre leviers de
  refus + PANs magiques.
- [subscriptions.fr.md](subscriptions.fr.md) — cycle de vie
  subscription (create → trigger-billing → cancel).
- [install.fr.md](install.fr.md) — comment atteindre Paysim depuis
  votre poste de test.
