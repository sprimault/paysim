> [🇬🇧 English](payzen.md) · [🇫🇷 Français](payzen.fr.md)

# Référence provider PayZen

**Version d'API visée** : Lyra REST V4.0. Voir
l'[index des fournisseurs](README.fr.md) pour les autres marques qui
tournent sur cette même passerelle.

Référence exhaustive du protocole PayZen tel que simulé par Paysim.
Documente chaque endpoint natif, chaque champ request/response, chaque
valeur autorisée, et là où Paysim se comporte différemment du vrai PSP —
plus les extensions Paysim greffées par-dessus.

Source de vérité côté code : `internal/providers/payzen/types.go` et
`handler.go`. Cette doc est maintenue à la main — une divergence avec
le code est un bug de la doc, pas une spec.

## Vue d'ensemble

**URL de base** (Paysim) :
`http://paysim:8080/api-payment/V4/*` — miroir du vrai PayZen
`https://api.payzen.eu/api-payment/V4/*`. Un marchand qui passe de la
sandbox PayZen à Paysim ne change que l'hôte de base.

**Authentification** : HTTP Basic. Toute paire user:pass non vide est
acceptée (Basic Auth permissive — le simulateur ne contrôle pas un vrai
accès, il logue juste l'utilisateur pour l'observabilité).

**Enveloppe** : chaque réponse suit l'enveloppe PayZen :
```json
{
  "status": "SUCCESS" | "ERROR",
  "answer": { … }
}
```
Le blob `answer` diffère par endpoint. En cas d'erreur, `answer` est
un `APIError` (voir [Codes d'erreur](#codes-derreur)).

**Content type** : `application/json` en requête, idem en réponse.

**Doc amont de référence** :
[PayZen REST V4](https://payzen.io/fr-FR/rest/V4.0/api/) et
[Lyra Collect](https://docs.lyra.com/fr/rest/V4.0/api/).

## Couverture des endpoints

### Simulés

| Méthode | Chemin                                       | Answer type                 |
| ------- | -------------------------------------------- | --------------------------- |
| POST    | `/api-payment/V4/Charge/CreatePayment`       | `CreatePaymentAnswer`       |
| POST    | `/api-payment/V4/Charge/UpdatePayment`       | `UpdatePaymentAnswer`       |
| POST    | `/api-payment/V4/Charge/CreateSubscription`  | `CreateSubscriptionAnswer`  |
| POST    | `/api-payment/V4/Transaction/Get`            | `TransactionGetAnswer`      |
| POST    | `/api-payment/V4/Subscription/Get`           | `SubscriptionGetAnswer`     |

### Non simulés (hors périmètre aujourd'hui)

Non-objectifs explicites — endpoints qu'un vrai client PayZen peut
appeler contre Paysim et qui retourneront `404` ou une erreur non
modélisée. Ouvrir une issue si un besoin réel se présente :

- `Transaction/Refund`, `Transaction/CancelOrRefund`, `Transaction/Update`
- `Subscription/Update`, `Subscription/Cancel` (natif — le cancel
  Paysim vit sur l'API générique,
  `POST /paysim/api/v1/subscriptions/{id}/cancel`)
- `Charge/SDKTest` (diagnostics SDK), `Charge/CreateToken` (création token seul)
- `PCI/Charge/CreatePayment` (saisie carte côté serveur, périmètre PCI)
- `Wallet/*`, `Customer/*`, `Order/*`, `Session/*`

## Endpoints natifs — détail

### Charge/CreatePayment

Crée un contexte de paiement et retourne un `formToken` que le
marchand passe au client SmartForm JavaScript (vrai PayZen) ou
réutilise via `paymentMethodToken` pour un paiement one-click
(extension Paysim, voir plus bas).

**Requête** — `CreatePaymentRequest` :

| Champ                | Type                | Requis | Notes                                                            |
| -------------------- | ------------------- | :----: | ---------------------------------------------------------------- |
| `orderId`            | string              |  oui   | Référence commande marchand, libre.                              |
| `amount`             | integer (centimes)  |  oui   | Dans la plus petite unité (centimes pour EUR).                   |
| `currency`           | string (ISO 4217)   |  oui   | Trois lettres majuscules (`EUR`, `USD`, …).                      |
| `formAction`         | string              |  non   | Voir valeurs autorisées ci-dessous.                              |
| `customer`           | `Customer`          |  non   | Infos acheteur — `email`, `reference`, `billingDetails`. `reference` est l'identifiant client côté marchand, restitué dans le `kr-answer`. |
| `metadata`           | `map[string]string` |  non   | Metadata marchand libre, propagée dans le webhook.               |
| `returnUrl`          | string              |  non   | Extension Paysim : cible de retour navigateur.                   |
| `notificationUrl`    | string              |  non   | Extension Paysim : cible du webhook IPN.                         |
| `paymentMethodToken` | string              |  non   | **Extension Paysim / PayZen** : rejeu one-click depuis un moyen de paiement enregistré. |
| `card`               | `Card`              |  non   | **Extension Paysim uniquement** (le vrai PayZen collecte la CB via le SmartForm client). Enregistre un moyen de paiement côté serveur. |

Valeurs `formAction` autorisées :

| Valeur              | Comportement                                                          |
| ------------------- | --------------------------------------------------------------------- |
| `PAYMENT` (défaut)  | Paiement one-shot, sans enregistrement.                               |
| `REGISTER_PAY`      | Paiement + enregistrement obligatoire du moyen de paiement.           |
| `ASK_REGISTER_PAY`  | Paiement + enregistrement proposé à l'utilisateur.                    |
| `REGISTER`          | Enregistrement seul, sans débit. `amount: 0` accepté — crée un `paymentMethodToken` réutilisable pour des paiements one-click. |

**Spécificités Paysim** :

- Le champ `card` **n'est pas dans le contrat PayZen réel** — en
  production, les données CB transitent par le SmartForm client
  (`kr-payment-form.min.js`), jamais par l'API marchand. Paysim
  l'accepte par commodité d'intégration. **L'alias n'est créé qu'une
  fois l'autorisation acceptée**, comme chez PayZen — « L'alias (token)
  ne sera pas créé si la demande d'autorisation ou de renseignement est
  refusée » :

  | Montant | Ce qui se passe | État du paiement | Quand l'alias apparaît |
  | --- | --- | --- | --- |
  | `0` | Vérification sans débit | `authorized` si acceptée, `declined` sinon | Tout de suite, dans la réponse de création |
  | `> 0` | Débit à jouer | `initiated` jusqu'au `simulate` | Au `simulate`, si l'issue est acceptée |

  La vérification est **autorisée, jamais capturée** : chez Lyra, cette
  transaction « n'est jamais remise en banque et reste dans l'onglet
  Transactions en cours ». Il y a eu demande d'autorisation, pas de
  mouvement de fonds. `PAYSIM_AUTOPLAY` ne la capture pas non plus — il
  joue ce qui attendait le porteur, il ne change pas ce qui sort.

  **Ce qui fait échouer une vérification tient au motif, pas au
  montant.** Une carte expirée échoue toujours. Un PAN de refus n'échoue
  que si son motif tient au statut de la carte :

  | PAN de test | Motif | Vérification à `0` |
  | --- | --- | --- |
  | `4000000000000002` | `51` provision insuffisante | **acceptée**, alias créé |
  | `5105105105105100` | `43` carte volée | refusée, aucun alias |
  | `2223000000000007` | `05` refus de l'émetteur | refusée, aucun alias |
  | `378282000000008` | `57` opération non permise | refusée, aucun alias |

  La provision passe parce qu'une vérification n'engage aucun montant :
  elle n'interroge pas le solde. La carte ne refusera qu'au premier
  débit — c'est le seul levier produisant un abonnement dont les
  échéances refusent pour ce motif, le montant d'une échéance étant
  imposé par l'échéancier et donc indisponible comme montant magique.

  Un paiement refusé ne laisse donc **aucun** alias, et la réponse de
  création d'un `REGISTER_PAY` ne porte pas encore de token — il faut
  relire le paiement, ou le lire dans le webhook. Voir
  [testing-cards.fr.md](../testing-cards.fr.md).
- `paymentMethodToken` en requête déclenche le mode **rejeu récurrent
  one-click** : pas de formulaire, capture directe (ou refus), issue
  synchrone, webhook IPN émis.
- **Montant magique `xxx03`** injecte une latence avant réponse (30 s),
  simule un timeout sans changer l'issue (voir
  [valeurs magiques](#valeurs-magiques-chaos)).

**Réponse** — `CreatePaymentAnswer` :

```json
{
  "status": "SUCCESS",
  "answer": { "formToken": "<32 caractères hex>" }
}
```

`formToken` est opaque côté marchand, 32 caractères hex, généré par
Paysim. Le marchand le passe au SmartForm ; sans rapport avec
`paymentMethodToken`.

### Charge/UpdatePayment

Met à jour le contexte d'un paiement existant (typiquement `customer`
après édition UI, ou `metadata`). N'affecte pas l'état domain.

**Requête** — `UpdatePaymentRequest` :

| Champ       | Type                | Requis |
| ----------- | ------------------- | :----: |
| `formToken` | string              |  oui   |
| `customer`  | `Customer`          |  non   |
| `metadata`  | `map[string]string` |  non   |

**Réponse** — même `formToken` (inchangé).

### Charge/CreateSubscription

Crée un abonnement natif PSP-driven. La subscription n'a **pas de
scheduler en fond** côté Paysim — voir
[subscriptions.fr.md](../subscriptions.fr.md) pour le choix
délibéré.

**Requête** — `CreateSubscriptionRequest` :

| Champ                | Type                | Requis | Notes                                        |
| -------------------- | ------------------- | :----: | -------------------------------------------- |
| `paymentMethodToken` | string              |  oui   | Doit référencer un moyen de paiement enrôlé. |
| `amount`             | integer (centimes)  |  oui   |                                              |
| `currency`           | string              |  oui   |                                              |
| `orderId`            | string              |  non   | Référence marchand.                          |
| `effectDate`         | string              |  non   | ISO 8601, date de la première échéance.      |
| `rrule`              | string              |  non   | RFC 5545 iCalendar (`RRULE:FREQ=MONTHLY;INTERVAL=1`). Stocké tel quel, **non consommé par un moteur interne**. |
| `metadata`           | `map[string]string` |  non   |                                              |

**Réponse** :

```json
{ "status": "SUCCESS", "answer": { "subscriptionId": "<uuid>" } }
```

### Transaction/Get

Retourne l'état d'une transaction indexée par UUID. Un UUID inconnu
produit un `200 OK` avec `status=ERROR` dans l'enveloppe
(`PAYSIM_UUID_UNKNOWN`) — respect du contrat PayZen qui n'utilise
jamais d'erreur HTTP pour un échec métier.

**Requête** — `TransactionGetRequest` :

| Champ  | Type   | Requis |
| ------ | ------ | :----: |
| `uuid` | string |  oui   |

**Réponse** — `TransactionGetAnswer` :

| Champ               | Type                | Notes                                       |
| ------------------- | ------------------- | ------------------------------------------- |
| `uuid`              | string              |                                             |
| `orderId`           | string              |                                             |
| `amount`            | integer (centimes)  |                                             |
| `currency`          | string              |                                             |
| `orderStatus`       | état domain         | `initiated` / `authorized` / `captured` / … |
| `paymentMethodType` | string              | Optionnel (`CARDS`, `IP_WIRE`, …).          |
| `creationDate`      | string              | ISO 8601 UTC (RFC 3339).                    |
| `lastUpdateDate`    | string              | Idem.                                       |
| `customer`          | `Customer`          | Si défini à la création.                    |
| `metadata`          | `map[string]string` |                                             |

### Subscription/Get

**Requête** — `SubscriptionGetRequest` :

| Champ                | Type   | Requis | Notes                                     |
| -------------------- | ------ | :----: | ----------------------------------------- |
| `subscriptionId`     | string |  oui   |                                           |
| `paymentMethodToken` | string |  oui   | Doit correspondre au moyen prélevé. Un couple incohérent répond comme un abonnement inconnu. |

**Réponse** — `SubscriptionGetAnswer` :

| Champ                | Type                | Notes                                    |
| -------------------- | ------------------- | ---------------------------------------- |
| `subscriptionId`     | string              |                                          |
| `orderId`            | string              |                                          |
| `amount`             | integer             |                                          |
| `currency`           | string              |                                          |
| `effectDate`         | string              |                                          |
| `rrule`              | string              |                                          |
| `paymentMethodToken` | string              |                                          |
| `creationDate`       | string              | ISO 8601.                                |
| `metadata`           | `map[string]string` |                                          |

## Endpoints de contrôle Paysim (hors PayZen)

Ces endpoints **n'existent pas** chez le vrai PayZen. Ils remplacent
ce que déclencheraient le SmartForm marchand et le backend PSP entre
eux. Sous un préfixe `/paysim/simulate/` pour rendre l'intention
lisible dans l'URL elle-même.

Bearer si `PAYSIM_API_TOKEN` configuré — voir
[install.fr.md](../install.fr.md).

### POST /paysim/simulate/browserReturn

Simule le POST navigateur qui suit un formulaire de paiement complété.
Paysim envoie un `kr-answer` signé vers `ReturnURL`.

**Requête** — `BrowserReturnRequest` :

| Champ               | Type               | Requis | Notes                                                  |
| ------------------- | ------------------ | :----: | ------------------------------------------------------ |
| `formToken`         | string             |  oui   |                                                        |
| `returnUrl`         | string             |  non   | Surcharge celle stockée à CreatePayment.               |
| `outcome`           | string             |  oui   | Voir [outcomes](#outcomes).                            |
| `paymentMethodType` | string             |  non   | Défaut `CARDS`.                                        |
| `cardBrand`         | string             |  non   | Défaut `VISA`.                                         |
| `wallet`            | string             |  non   | `APPLE_PAY`, `GOOGLEPAY`, vide. Voir [portefeuilles](#les-portefeuilles-ne-sont-pas-simulés). |
| `threeDSStatus`     | string             |  non   | `SUCCESS` (défaut) / `CHALLENGE` / `FAILURE` / `NOT_ENROLLED`. |
| `errorCode`         | string             |  non   | Pour `outcome=UNPAID`.                                 |
| `errorMessage`      | string             |  non   |                                                        |
| `chaos`             | `WebhookChaos`     |  non   | Voir [valeurs magiques](#valeurs-magiques-chaos).      |
| `deliveryDelayMs`   | integer            |  non   | Retarde la livraison du webhook (millisecondes).       |

**Réponse** — `BrowserReturnResponse` :
```json
{ "status": "SUCCESS", "deliveryId": "<uuid>", "krHash": "<hex>" }
```

### POST /paysim/simulate/ipn

Identique à `browserReturn`, mais le POST résultant cible
`notificationUrl` au lieu de `returnUrl` — le webhook
serveur-à-serveur côté marchand.

## Structure kr-answer

Payload complet envoyé dans le champ POST `kr-answer` au marchand
(retour navigateur ou IPN). Signé via
[`kr-hash`](#signature-kr-hash).

```
KrAnswer
├── shopId              string (optionnel)
├── orderCycle          string
├── orderStatus         string       "PAID" | "UNPAID" | …
├── serverDate          string       ISO 8601
├── serverUrl           string (optionnel)
├── applicationVersion  string (optionnel)
├── applicationProvider string (optionnel) — code de marque, voir plus bas
├── mode                string       "TEST" (Paysim n'émet jamais "PRODUCTION")
├── orderDetails
│   ├── orderTotalAmount     integer
│   ├── orderCurrency        string
│   ├── mode                 string
│   ├── orderId              string
│   ├── orderEffectiveAmount integer
│   └── _type                "V4/OrderDetails"
├── customer            Customer (optionnel)
├── transactions[]      une entrée par paiement (une seule aujourd'hui)
│   ├── uuid                 string
│   ├── amount               integer
│   ├── currency             string
│   ├── paymentMethodType    string
│   ├── paymentMethodToken   string (présent quand un alias a été créé ou rejoué)
│   ├── paymentMethodTokenStatus  "ACTIVE" | "CANCELLED" (avec le token)
│   ├── status               "PAID" | "UNPAID"
│   ├── detailedStatus       "AUTHORISED" | "CAPTURED" | "REFUSED" | …
│   ├── operationType        "DEBIT" | "CREDIT"
│   ├── creationDate         string
│   ├── errorCode            string (optionnel)
│   ├── errorMessage         string (optionnel)
│   ├── metadata             map[string]string (optionnel)
│   ├── transactionDetails
│   │   ├── mid              string (optionnel)
│   │   ├── creationContext  "CHARGE"
│   │   ├── wallet           string (optionnel)
│   │   ├── cardDetails      (CARDS/CB uniquement, voir plus bas)
│   │   ├── threeDSResponse  (voir plus bas)
│   │   └── _type            "V4/TransactionDetails"
│   └── _type                "V4/PaymentTransaction"
├── subscriptionId      string (optionnel)
└── _type               "V4/Payment"
```

`cardDetails` :

```
KrCardDetails
├── pan               string     toujours masqué (ex. "411111XXXXXX1111")
├── brand             string
├── holderName        string     omis si non fourni à l'enrôlement
├── productCategory   string     "CREDIT" par défaut
├── expiryMonth       integer
├── expiryYear        integer
├── country           string     "FR" par défaut
├── issuerName        string     "PAYSIM" par défaut
├── effectiveBrand    string
└── _type             "V4/CardDetails"
```

**Tous ces champs sont dérivés de la carte réellement enrôlée** quand il
y en a une — l'objet `card` de `CreatePayment`, conservé comme moyen de
paiement. Ce que Paysim annonce est donc ce que Paysim détient : PAN
masqué, date d'expiration, porteur et attributs émetteur correspondent à
l'enregistrement stocké.

Les valeurs signalées *par défaut* ne s'appliquent que si l'enrôlement
les a laissées vides. Elles ne se substituent **jamais** à une valeur
fournie : enrôlez une carte avec `country: "US"` et
`productCategory: "DEBIT"`, le webhook annonce exactement cela — c'est ce
qui rend testables la carte étrangère et la carte de débit.

Le paiement one-shot sans aucune carte soumise est le seul cas où le bloc
entier est synthétique : il n'y a rien de réel à décrire, Paysim émet
alors une carte de démonstration construite à partir de la seule marque.

`threeDSResponse` :

```
KrThreeDSResponse
├── authenticationResultData
│   ├── status              "SUCCESS" | "FAILURE" | "NOT_ENROLLED" | "UNAVAILABLE"
│   ├── authenticationType  "FRICTIONLESS" | "CHALLENGE" (issu du threeDSStatus demandé)
│   └── _type               "V4/AuthenticationResultData"
└── _type                   "V4/ThreeDSResponse"
```

### `applicationProvider` nomme la marque

Chaque `kr-answer` porte le code de marque de l'instance qui l'a émis. Il
ne se déduit pas du nom d'hôte — quatre hôtes distincts annoncent
`PAYZEN` — d'où une table explicite, relevée sur les API de production en
août 2026 :

| Marque | `applicationProvider` |
| --- | --- |
| PayZen | `PAYZEN` |
| Systempay | `NPS` |
| Sogecommerce | `SOGECOM` |
| Scellius | `LBP` |
| Lyra Collect | `LYRA` |

Une marque inconnue rend une valeur vide, donc un champ omis : mieux vaut
ne rien annoncer qu'annoncer la mauvaise marque. C'est ce qui permet à une
instance d'héberger plusieurs intégrations en laissant chacune reconnaître
ses propres webhooks.

## Les portefeuilles ne sont pas simulés

Le champ `wallet` porte une étiquette, rien de plus.

Un vrai paiement Apple Pay ou Google Pay se joue dans le navigateur : le SDK
Krypton servi depuis `static.payzen.eu`, `ApplePaySession` ou l'API Google
Pay, un domaine validé chez Apple, un certificat marchand, un appareil
compatible. Rien de tout cela n'est à portée d'un simulateur qui ne sert pas
le SDK — et le SDK est délibérément hors périmètre, puisqu'il appartient au
fournisseur et que vous l'utilisez tel quel.

Ce que Paysim offre, c'est le versant serveur : annoncer un portefeuille dans
le `kr-answer` pour exercer votre code — le libellé affiché, la règle de
réconciliation, le contrôle antifraude qui traite un portefeuille à part.

**Sa forme exacte n'est attestée par aucune capture réelle.** `testdata/raw`
est vide : le champ est émis tel que documenté ici sur la foi d'une lecture,
non d'un `kr-answer` enregistré, et une source au moins laisse entendre que
le portefeuille apparaîtrait plutôt concaténé au type de carte. À prendre
comme une commodité pour exercer votre propre code, pas comme une
reproduction fidèle de ce qu'émet la vraie plateforme. Si vous disposez d'une
capture authentique d'un paiement par portefeuille,
[ouvrez une issue](https://github.com/sprimault/paysim/issues) — c'est
exactement le type de vecteur que ce projet demande plutôt que d'inventer.

## Outcomes

Valeurs acceptées dans le champ `outcome` de `browserReturn` / `ipn` :

| Valeur       | Effet domain                                 | Webhook status | Webhook detailedStatus |
| ------------ | -------------------------------------------- | :------------: | :--------------------: |
| `PAID`       | `Capture()` — fonds débités                  | `PAID`         | `CAPTURED`             |
| `AUTHORISED` | `Authorize()` — fonds réservés, non débités  | `PAID`         | `AUTHORISED`           |
| `UNPAID`     | `Decline(reason)` — refus bancaire           | `UNPAID`       | `REFUSED`              |
| `EXPIRED`    | `Expire()` — timeout                         | `UNPAID`       | `EXPIRED`              |
| `ABANDONED`  | Mappé vers `Expire()` (pas d'état domain)    | `UNPAID`       | `ABANDONED`            |

### Les champs client vides sortent en `null`

PayZen expose les blocs du contexte client **en entier**, champs compris, valorisés à
`null` quand ils sont vides — et non sous forme d'objet vide :

```json
"customer": {
  "email": null,
  "reference": null,
  "billingDetails": { "address": null, "firstName": null, "city": null, … },
  "shippingDetails": { "address": null, "category": null, … },
  "extraDetails": { "ipAddress": null, … }
}
```

Paysim s'y aligne. Deux écarts sont comblés d'un coup :

- **La clé absente** — l'écart structurel. `Object.keys()`, l'itération, un `in` et un
  typage non optionnel divergent tous quand une clé manque. C'est ce qui produit un
  « ça marchait en test ».
- **`""` au lieu de `null`** — plus discret, mais réel : `firstName ?? "N/A"` rend
  `"N/A"` chez PayZen et `""` ici, puisque la chaîne vide n'est pas *nullish*.

Les champs restent de simples chaînes dans le modèle — aucun pointeur, aucun
déréférencement — et la conversion se fait à la sortie seulement. Le décodage est
inchangé : `null` donne la valeur zéro, comme toujours.

## L'alias porte son client

Un `paymentMethodToken` — un *alias*, dans le vocabulaire de PayZen — appartient à un
**client**, jamais à une commande. Cette relation a une conséquence que Paysim reproduit
désormais :

> Lors d'un paiement par alias, les attributs `customer.reference`, `customer.email` et
> `customer.billingDetails` transmis dans la requête sont **ignorés**, et les valeurs
> associées à l'alias sont utilisées.

```
Enrôlement    : token T, customer.reference = "client-A"
Débit par T   : customer.reference = "client-B"   ← erreur côté marchand

PayZen répond : "client-A"    (l'alias gagne, le bug reste invisible)
Paysim répond : "client-A"    (idem — depuis la v0.5.4)
```

Avant la v0.5.4, Paysim restituait ce que contenait la requête. Il était donc *plus
logique* que le vrai — et par là trompeur : une référence client erronée passait la
validation contre Paysim, puis dérivait silencieusement en production. Reproduire le
protocole tel qu'il est, défauts compris, c'est l'invariant 3.

**`shippingDetails` et `extraDetails` ne sont pas écrasés.** Une adresse de livraison
appartient à la commande — on livre à des endroits différents avec la même carte — et le
contexte navigateur à la session. PayZen ne prétend pas les remplacer non plus.

Les alias enrôlés avant la v0.5.4 ne portent aucun client : le débit retombe alors sur
celui de la requête, faute de mieux.

## Motifs de refus

Un marchand ne traite pas tous les refus de la même façon : une provision insuffisante
se retente dans quelques jours, une opposition impose de réclamer une autre carte tout
de suite. Sans motif dans la réponse, cette logique de reconduction ne peut ni s'écrire
ni se tester — elle part en production à l'aveugle, et se découvre par un client
suspendu à tort.

Une transaction refusée porte son motif dans `detailedErrorCode`, accompagné d'un
libellé lisible :

```json
"transactions": [{
  "status": "UNPAID",
  "detailedStatus": "REFUSED",
  "errorCode": "PAYSIM_REFUSED",
  "detailedErrorCode": "51",
  "detailedErrorMessage": "provision insuffisante"
}]
```

| Code | Signification                     | Réaction marchande typique    |
| :--: | --------------------------------- | ----------------------------- |
| `51` | Provision insuffisante            | retenter dans quelques jours  |
| `43` | Carte volée, opposition           | réclamer un autre moyen       |
| `91` | Émetteur inaccessible             | retenter rapidement           |
| `05` | Refus de l'émetteur (générique)   | aucun signal exploitable      |
| `57` | Opération non permise au porteur  | réclamer un autre moyen       |

**Ce sont les codes de retour d'autorisation ISO 8583**, pas des valeurs Paysim : c'est
ce que l'acquéreur remonte et que PayZen relaie tel quel. Les reproduire à l'identique
est le seul moyen qu'un mapping écrit contre Paysim reste valable en production. Noter
le contraste avec `errorCode`, lui préfixé `PAYSIM_` : celui-là est un code de niveau
PSP, et y inventer une valeur d'allure PayZen reviendrait à se faire passer pour le vrai.

**Paysim n'interprète jamais ces codes et n'expose aucun booléen « rejouable ».**
Décider qu'un `51` se retente et qu'un `43` non est une politique marchande, pas une
donnée du protocole. Ajouter ce verdict vous donnerait une sémantique que le vrai ne
fournit pas — et votre logique de reconduction serait alors écrite contre nous plutôt
que contre PayZen.

Deux leviers fixent le motif : le **montant magique** sur le parcours navigateur, le
**PAN de test** sur le récurrent, où le montant est imposé par l'abonnement. Voir
[testing-cards.fr.md](../testing-cards.fr.md).

### Le même motif porte deux noms

`detailedErrorCode` est le nom **PayZen**, celui du `kr-answer`. L'API de contrôle de
Paysim, elle, expose la même donnée à plat sous deux champs :

| Où on lit                                   | Code                | Libellé                  |
| ------------------------------------------- | ------------------- | ------------------------ |
| `kr-answer` PayZen (`transactions[0]`)       | `detailedErrorCode` | `detailedErrorMessage`   |
| API de contrôle (`/paysim/api/v1/payments`)  | `declineCode`       | `declineMessage`         |

Ce n'est pas une réécriture du protocole : l'API de contrôle n'est pas PayZen, et lui
faire porter un nom PayZen laisserait croire qu'elle en imite le format. Mais coder
d'après cette page sans connaître le second couple de noms fait chercher un champ qui
n'existe pas — et un motif absent ne lève rien, il reste vide en silence.

Les deux couples sont présents partout où un refus se lit : dans le sommaire d'un
paiement, dans son détail, et **dès la réponse de création** quand l'issue est immédiate
(rejeu one-click, autoplay). Aucune relecture n'est nécessaire pour connaître le motif.

Un refus sans motif bancaire laisse les deux champs vides — c'est le cas d'un abandon,
d'une expiration, ou d'un moyen révoqué : là, ce n'est pas un émetteur qui refuse, et
inventer un code d'autorisation reviendrait à annoncer une décision bancaire qui n'a pas
eu lieu.

## Valeurs magiques (chaos)

Paysim embarque deux catégories de tweaks — cf.
[testing-cards.fr.md](../testing-cards.fr.md).

**Par montant (magic amounts)** — s'appliquent au simulate/charge :

| Montant terminant par | Effet                                                          |
| --------------------- | -------------------------------------------------------------- |
| `01` (centimes)       | Force `UNPAID`, motif `51` — provision insuffisante.           |
| `02` (centimes)       | Force `UNPAID`, motif `43` — carte volée, opposition.          |
| `03` (centimes)       | Latence 30 s injectée sur la réponse `CreatePayment` (test timeout). |
| `04` (centimes)       | Force `UNPAID`, motif `91` — émetteur inaccessible.            |

**Par PAN (magic PANs)** — PANs de test Luhn-valides réservés pour
refus systématique :

| Marque             | PAN                |
| ------------------ | ------------------ |
| Visa               | `4000000000000002` |
| Mastercard         | `5105105105105100` |
| Mastercard série 2 | `2223000000000007` |
| American Express   | `378282000000008`  |

**Par struct (`chaos` dans simulate)** — chaos webhook ciblé
par requête :

| Champ                | Type    | Effet                                                            |
| -------------------- | ------- | ---------------------------------------------------------------- |
| `duplicate`          | boolean | Enqueue le webhook deux fois (test idempotence).                 |
| `badSignature`       | boolean | Envoie un `kr-hash` cassé — le marchand doit refuser.            |
| `raceBeforeResponse` | boolean | Retarde la réponse HTTP simulate de 500 ms — webhook part avant. |

Plus `deliveryDelayMs` (integer, délai de livraison en ms).

## Codes d'erreur

Préfixe `PAYSIM_*` pour ne pas se confondre avec les codes PayZen réels
(`INT_010`, `PSP_010`, `ACQ_010`, …). Retournés dans
`APIError.errorCode`.

| Code                             | Sens                                                |
| -------------------------------- | --------------------------------------------------- |
| `PAYSIM_INVALID_REQUEST`         | JSON mal formé, champ requis manquant, input invalide. |
| `PAYSIM_INVALID_AMOUNT`          | Montant nul, négatif, ou dépassement.               |
| `PAYSIM_INVALID_CURRENCY`        | Pas ISO 4217 majuscule 3 lettres.                   |
| `PAYSIM_INVALID_PAYMENT`         | Invariant domain violé (transition invalide).       |
| `PAYSIM_UUID_UNKNOWN`            | `Transaction/Get` sur UUID inconnu.                 |
| `PAYSIM_TOKEN_UNKNOWN`           | `UpdatePayment` sur formToken inconnu.              |
| `PAYSIM_SUBSCRIPTION_UNKNOWN`    | `Subscription/Get` sur id inconnu.                  |
| `PAYSIM_STORE_FAILURE`           | Erreur du store sous-jacent (SQLite plein, corruption). |
| `PAYSIM_PAYMENT_METHOD_UNKNOWN`  | Rejeu one-click sur `paymentMethodToken` inconnu.   |

Un alias expiré ou révoqué ne produit **aucune erreur d'API**.
`CreatePayment` répond `SUCCESS` avec un `formToken`, et le refus arrive
là où arrive un vrai refus — dans le webhook :

```
orderStatus     UNPAID
status          UNPAID
detailedStatus  REFUSED
errorCode       PAYSIM_REFUSED
errorMessage    moyen de paiement revoque | moyen de paiement expire
```

C'est délibéré, et c'est ce que fait la vraie plateforme : une carte
refusée par l'émetteur est un paiement refusé, pas une requête malformée.
Du code écrit contre une erreur d'API pour ces deux cas ne s'exécuterait
jamais en production.

## Signature kr-hash

Reproduit le contrat PayZen byte-pour-byte — validé contre le
[SDK Java officiel Lyra](https://github.com/lyra/rest-api-server-java-sdk).

- **Algorithme** : HMAC-SHA-256, encodage hex minuscule (pas base64).
- **Message** : contenu brut du champ `kr-answer`, chaîne JSON exacte,
  byte-pour-byte.
- **Champs POST accompagnant `kr-answer`** :
  - `kr-hash` : la signature.
  - `kr-hash-algorithm` : `sha256_hmac`.
  - `kr-hash-key` : **`sha256_hmac` ou `password` selon le canal**, voir
    ci-dessous.
  - `kr-answer-type` : `V4/Payment`.
- **Vérification** : temps constant (`hmac.Equal`), jamais `==`.

### Deux clés, une par canal

PayZen ne signe pas ses deux canaux avec la même clé. Il annonce
laquelle il a employée dans `kr-hash-key`, et c'est ce champ — non le
canal deviné — qui doit décider côté marchand :

| `kr-hash-key` | Canal | Clé côté Paysim | Clé côté marchand |
| --- | --- | --- | --- |
| `sha256_hmac` | Retour navigateur | `PAYSIM_PAYZEN_HMAC_KEY` | clé HMAC de la boutique |
| `password` | Notification serveur (IPN) | `PAYSIM_PAYZEN_REST_PASSWORD` | mot de passe d'API REST |

Le SDK PHP officiel procède exactement ainsi :

```php
if ($_POST['kr-hash-key'] == "sha256_hmac") { $key = $this->_hashKey; }
elseif ($_POST['kr-hash-key'] == "password") { $key = $this->_password; }
```

**Vérifier une notification avec la clé du navigateur échoue.** C'est
la panne qu'un intégrateur emporte en production s'il n'a codé qu'une
seule branche — elle passait avant la v0.6.3, où Paysim annonçait
`sha256_hmac` partout et signait tout avec la même clé.

Les deux valeurs peuvent être identiques en local ; ce qui compte est
que le marchand emprunte le chemin de vérification que la production
empruntera.

**Vecteur de validation** (extrait du SDK Java Lyra) :

- Clé : `ktM7bSeTJpclvpm4eEE9N0LIyoxUvsQ9AAYbQI1xQx7Qh`
- Message : chaîne vide `""`
- HMAC-SHA-256 hex : `a95c2b13d50d57858ff38e7abd76c39d644fd5d1cfdcc360e4c61f2fc48d4a5e`

Les libs PHP OSS historiques (nursit, thelia) concatènent la clé au
message ; Paysim suit le comportement du SDK officiel Lyra à la place —
HMAC(message, key) standard, sans concaténation.

## Voir aussi

- [testing-cards.fr.md](../testing-cards.fr.md) — leviers de refus et PANs magiques.
- [subscriptions.fr.md](../subscriptions.fr.md) — cycle de vie subscription.
- [scenarios.fr.md](../scenarios.fr.md) — DSL YAML pour les tests d'intégration.
- [install.fr.md](../install.fr.md) — déploiement et configuration.
