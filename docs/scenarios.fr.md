> [🇬🇧 English](scenarios.md) · [🇫🇷 Français](scenarios.fr.md)

# Scénarios

Paysim embarque un DSL YAML de scénarios et un runner (`paysim run
scenario.yml`) pour qu'un test d'intégration puisse enchaîner une
séquence d'étapes contre Paysim et asserter l'issue. Même runner en
local, en CI, ou contre un Paysim déployé sur un cluster — seule
`PAYSIM_URL` change.

> **Exemples shell** : les blocs `bash` ci-dessous supposent Git Bash
> sous Windows ou un shell POSIX. Pour PowerShell natif, remplacer
> `VAR=value cmd` par `$env:VAR="value"; cmd`.

Les exemples canoniques vivent dans
[examples/scenarios/](../examples/scenarios/) — sept fichiers courts
qui couvrent one-shot, token pattern, subscription native et
enrôlement pur.

## Comment lancer

```bash
export PAYSIM_URL=http://paysim:8080
paysim run scenarios/one-shot.yml
```

Codes de retour (adaptés à la CI) :

| Code | Sens                                                              |
| :--: | ----------------------------------------------------------------- |
|  0   | Toutes les étapes ont passé.                                      |
|  1   | Assertion échouée (`assert_state`, `assert_webhook`, `assert_subscription`, `assert_payment_method`, `assert_customer`). |
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

> **Deux vocabulaires, à ne pas confondre.** Ce DSL YAML est en
> `snake_case` (`order_id`, `expiry_month`, `form_action`) ; l'API HTTP,
> elle, attend du `camelCase` (`orderId`, `expiryMonth`, `formAction`).
>
> Recopier un champ YAML dans un `curl` ne produit pas d'erreur de
> syntaxe : le champ inconnu est simplement ignoré. Un `order_id` passé
> en JSON donne un paiement sans référence de commande, et un
> `expiry_month` donne une carte sans date — désormais refusée en `400`,
> ce qui au moins le signale.

## Référence des actions

Treize actions qui couvrent les trois patterns de paiement.

### Paiements one-shot

| Action           | Rôle                                                                    |
| ---------------- | ----------------------------------------------------------------------- |
| `create_payment` | Crée un paiement. `card`, `form_action`, `customer` (voir plus bas), `metadata`, `notification_url` optionnels. `amount: 0` valide quand `form_action: REGISTER` (enrôlement pur, aucun débit). |
| `simulate`       | Fait avancer le paiement via l'endpoint de simulation navigateur.       |
| `assert_state`   | Assert que le paiement courant est dans l'état demandé.                 |
| `assert_webhook` | Compte les webhooks livrés depuis le début du scénario (`status`, `outcome`, `timeout` optionnels).|
| `assert_customer` | Vérifie le contexte marchand restitué par le paiement courant, sous `expect` — même forme que `customer` sur `create_payment`. `uuid` optionnel (défaut : dernier paiement). Seuls les champs renseignés sont comparés. |

### Récurrence par token

| Action         | Rôle                                                                             |
| -------------- | -------------------------------------------------------------------------------- |
| `charge_token` | Déclenche un prélèvement récurrent one-click via le dernier `paymentMethodToken` enrôlé. `token` optionnel (défaut `currentToken`). |
| `assert_payment_method` | Vérifie ce qui a réellement été enregistré à l'enrôlement. `token` optionnel (défaut `currentToken`). Tous les champs de contrôle sont optionnels — seuls ceux renseignés sont comparés : `brand`, `pan_masked`, `holder_name`, `country`, `product_category`, `issuer_name`, `usable`, `unusable_reason`. Une assertion sans aucun champ est rejetée au chargement : elle passerait toujours. |

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
| `advance_time` | Avance l'horloge du simulateur de `duration`, sans dormir. |
| `reset_time` | Ramène l'instance à l'heure réelle. Sans payload. |
| `inject`| Empile un mode chaos consommé par le **prochain** `simulate`. |

`wait` et `advance_time` sont exactement inverses : le premier dort sans
faire vieillir l'instance — il laisse une livraison arriver — le second
fait vieillir sans dormir. C'est ce qui rend testable en CI ce qui se
mesure en jours : un alias qui expire, une échéance qui tombe.

```yaml
- action: advance_time
  duration: 720h        # trente jours ; les avances se cumulent
```

Le recul est refusé au chargement : un scénario qui remonte le temps
échoue avant de commencer. Pour revenir en arrière, il y a `reset_time`.

**Un scénario qui avance le temps le remet en place.** L'instance est
partagée entre les scénarios d'une même exécution : la laisser en avant
fausserait les suivants, dont les assertions « depuis le début du
scénario » compteraient alors les livraisons de celui-ci.

```yaml
- action: advance_time
  duration: 1440h
- action: assert_payment_method
  usable: false
- action: reset_time
```

Modes `inject` reconnus (one-shot — consommé par le prochain `simulate`,
puis remis à zéro) :

| Mode              | Effet sur le webhook déclenché par le prochain `simulate`             |
| ----------------- | --------------------------------------------------------------------- |
| `duplicate`       | Webhook enqueue deux fois (test idempotence côté marchand).           |
| `bad-signature`   | `kr-hash` altéré — le marchand qui vérifie la signature doit refuser. |
| `bad-algorithm`   | `kr-hash-algorithm` annonce un algorithme inconnu, la signature restant valide. Le SDK marchand lève au lieu de comparer — la branche que personne ne teste. |
| `race`            | Réponse HTTP simulate retardée 500 ms ; le webhook part en premier.   |
| `delay=NNN`       | Retarde la livraison du webhook de NNN millisecondes (compose avec un second `simulate` pour tester le out-of-order). |

Chaos persistant : réinjecter avant chaque `simulate` concerné. Voir
`examples/scenarios/chaos-duplicate.yml`.

### `status` et `outcome`

`assert_webhook` filtre sur deux choses indépendantes, et les confondre
revient à asserter autre chose que ce qu'on croit :

| Champ | Répond à | Valeurs |
|---|---|---|
| `status` | *Le webhook est-il arrivé ?* | `delivered`, `failed`, `pending` |
| `outcome` | *Qu'annonçait-il ?* | `PAID`, `UNPAID`, `AUTHORISED`… (vocabulaire provider) |

Un webhook remis avec succès peut parfaitement annoncer un refus — un
HTTP 200 sur un payload `UNPAID`. C'est en général le résultat métier
qu'on veut asserter :

```yaml
  - action: assert_webhook
    count: 1
    outcome: PAID          # un paiement accepté a bien été annoncé
```

Les deux se cumulent : fournir l'un et l'autre exige que les deux
correspondent. Les omettre compte tous les webhooks.

`outcome` est renseigné par l'adaptateur au moment d'émettre le webhook,
dans son propre vocabulaire de protocole — jamais relu depuis le corps.
Un adaptateur Stripe alimentera le même champ avec ses valeurs.

### Pourquoi `assert_webhook` attend

La livraison d'un webhook est asynchrone : le handler enqueue et répond,
le worker livre et historise ensuite. `assert_webhook` interroge donc
l'API jusqu'à atteindre le compte attendu, et n'échoue qu'au bout d'un
délai — **5 secondes** par défaut. Un scénario dont le compte est juste
sort au premier tour et ne coûte rien.

Relever `timeout` quand un `inject` a retardé la livraison au-delà :

```yaml
  - action: inject
    mode: delay=8000
  - action: simulate
    status: captured
  - action: assert_webhook
    count: 1
    timeout: 12s
```

À noter : `count: 0` retourne immédiatement — il assert que rien n'a
*encore* été livré, pas que rien ne le sera jamais.

## L'objet `card`

Fournir `card` sur un `create_payment` enrôle un moyen de paiement et
retourne un `paymentMethodToken` réutilisable. Seuls les trois premiers
champs sont obligatoires :

```yaml
card:
  pan: "4111111111111111"
  expiry_month: 12
  expiry_year: 2028
  brand: VISA                  # optionnel, déduit du BIN si absent
  holder_name: DUPONT JEAN     # optionnel
  country: US                  # optionnel, ISO 3166-1 alpha-2, défaut "FR"
  product_category: DEBIT      # optionnel, CREDIT | DEBIT | PREPAID
  issuer_name: BANQUE DE TEST  # optionnel
```

Ces valeurs sont conservées avec le moyen de paiement et restituées
telles quelles dans le bloc `cardDetails` de chaque webhook que le token
produira ensuite. C'est ce qui rend testables la carte étrangère, la
carte de débit et le routage par émetteur — les quatre derniers champs
étaient auparavant figés à `FR` / `CREDIT` / `PAYSIM`.

**N'utilisez jamais un numéro de carte réel** : les PAN sont stockés en
clair. Voir [testing-cards.fr.md](testing-cards.fr.md).

## L'objet `customer`

Le contexte marchand, restitué tel quel dans le webhook. Paysim ne
l'interprète jamais — il doit seulement le rendre intact, ce que les
scénarios sont précisément là pour prouver.

```yaml
customer:
  email: alice@example.com
  reference: demo-org          # identifiant client côté marchand
  billing_details:
    first_name: Alice
    last_name: MARTIN
    address: 1 rue de la Paix
    zip_code: "75002"
    city: Paris
    country: FR
    language: fr
  shipping_details:
    category: COMPANY          # PRIVATE | COMPANY
    legal_name: ACME SARL      # COMPANY uniquement
    identity_code: "12345678900011"
    first_name: Bob            # on livre souvent à un autre que le payeur
    last_name: DURAND
    phone_number: "+33600000000"
    street_number: "12"
    address: avenue des Champs
    address2: batiment C
    district: 8e
    zip_code: "75008"
    city: Paris
    state: IDF
    country: FR
    delivery_company_name: TRANSPORTEUR X
    shipping_speed: EXPRESS    # STANDARD | EXPRESS | PRIORITY
    shipping_method: RELAY_POINT
  extra_details:
    ip_address: 203.0.113.7
    finger_print_id: fp-abc123
    browser_user_agent: Mozilla/5.0
    browser_accept: text/html
```

Tous les champs sont optionnels. Les noms reprennent ceux de PayZen —
`shipping_details` est découpé plus finement que `billing_details` parce
que les règles antifraude comparent ses éléments un à un.

`category`, `shipping_speed` et `shipping_method` ne sont **pas
validés** : un simulateur qui refuserait une valeur que le vrai accepte
serait un piège. `shipping_method` compte à lui seul une quinzaine de
valeurs en amont (`RELAY_POINT`, `DIGITAL_GOOD`, `PICKUP_POINT`,
`ETICKET`…), et cette liste bouge.

`extra_details` porte le contexte navigateur que PayZen transmet à ses
règles antifraude et à 3DS — le bloc à renseigner pour scripter un refus
pour risque.

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
Quand Stripe arrivera (`provider: stripe`), les scénarios
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
- `register-only.yml` — enrôlement pur de carte (`form_action: REGISTER`,
  `amount: 0`), avec propagation de `customer.email` et `metadata`
  jusqu'au webhook, puis un `charge_token` qui prouve que le token
  enregistré est réutilisable. `assert_payment_method` contrôle la
  marque et les attributs du porteur réellement retenus ; la carte est
  une Mastercard pour que la vérification soit discriminante — `VISA`
  est la valeur de repli du kr-answer.
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
