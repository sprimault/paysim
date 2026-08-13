> [🇬🇧 English](testing-cards.md) · [🇫🇷 Français](testing-cards.fr.md)

# Cartes de test et scénarios de refus

> **Exemples shell** : les blocs `curl` ci-dessous supposent Git Bash
> sous Windows ou un shell POSIX. Pour PowerShell natif, utiliser
> `Invoke-RestMethod` avec les arguments équivalents (`-Method`,
> `-Uri`, `-Body`, `-ContentType`).

Paysim embarque quatre numéros de carte de test réservés qui déclenchent
un refus systématique lors d'un rejeu récurrent (`charge_token`).
Combinés à trois autres leviers indépendants, ils permettent à un client
d'intégration (pipelines CI, tests manuels) de reproduire de
manière déterministe tous les scénarios d'échec de paiement, sans
dépendre d'un état particulier côté serveur.

## Cartes réservées de refus

Chaque marque expose un numéro Luhn-valide que Paysim reconnaît comme un
« refus » sur tout appel `charge_token`. Le complément est composé de
zéros terminés par le check digit Luhn correct — valeur mémorisable et
scriptable.

| Marque               | Préfixe  | Longueur | PAN de test         | Motif de refus                  |
| -------------------- | -------- | :------: | ------------------- | ------------------------------- |
| Visa                 | `400000` |    16    | `4000000000000002`  | `51` provision insuffisante     |
| Mastercard           | `510510` |    16    | `5105105105105100`  | `43` carte volée, opposition    |
| Mastercard série 2   | `222300` |    16    | `2223000000000007`  | `05` refus de l'émetteur        |
| American Express     | `378282` |    15    | `378282000000008`   | `57` opération non permise      |

Chaque PAN porte un **motif fixe**, restitué dans `detailedErrorCode`. C'est
ce qui compte sur un prélèvement récurrent : le montant y est imposé par
l'abonnement, on ne peut pas le tordre pour choisir un motif — le PAN est le
seul levier qui reste sur ce chemin.

**Portée** : la reconnaissance agit dès qu'un `PaymentMethod` est
associé au paiement. Ça couvre le **premier paiement où une carte a
été fournie** (l'appel simulate refuse, quel que soit l'outcome
demandé) et le **rejeu récurrent via un token stocké**
(`charge_token` refuse directement). Aucune cérémonie d'enrôlement
n'est requise — dès qu'on attache une `card` à un `POST /payments`,
Paysim la stocke et vérifie le PAN à chaque étape suivante.

## Les quatre leviers

Chaque levier cible un moment ou un défaut différent et se compose avec
les autres. Tous sont opt-in : le comportement par défaut est un
paiement accepté.

| Levier            | Moment            | Cas de test typique                            |
| ----------------- | ----------------- | ---------------------------------------------- |
| Montant magique     | au `simulate`                            | Refus bancaire pendant le parcours utilisateur   |
| PAN magique         | au `simulate` **et** au `charge_token`   | Refus bancaire sur tout paiement d'une CB donnée |
| Alias périmé        | au `simulate` **et** au `charge_token`   | Échéance qui tombe après la date d'expiration    |
| Révocation manuelle | au `simulate` **et** au `charge_token`   | Moyen de paiement supprimé après l'enrôlement    |

### Montant magique — refus au simulate

Trois terminaisons forcent `simulate` à retourner `UNPAID`, chacune avec son
motif :

| Montant se termine par | `detailedErrorCode` | Signification            | Réaction marchande        |
| :--------------------: | :-----------------: | ------------------------ | ------------------------- |
| `01`                   | `51`                | Provision insuffisante   | retenter dans quelques jours |
| `02`                   | `43`                | Carte volée              | réclamer une autre carte  |
| `04`                   | `91`                | Émetteur inaccessible    | retenter rapidement       |

`1001`, `2001`, `12301` refusent tous avec `51`. Les terminaisons en `03`
sont prises par le levier de latence et ne changent pas l'issue.

### PAN magique — refus sur tout paiement

Attacher une carte avec un des quatre PANs ci-dessus au premier
paiement. L'appel `simulate` refuse même quand l'`outcome` demandé
est `PAID` ; un `charge_token` ultérieur sur le token stocké refuse
également. Fonctionne indépendamment du montant et de tout état
préalable.

### Carte expirée — refus dès la présentation

Une carte déjà expirée **ne s'enrôle pas** : la demande d'autorisation
est refusée, et « L'alias (token) ne sera pas créé si la demande
d'autorisation ou de renseignement est refusée ». Présentée à un
paiement, elle le fait refuser sans laisser d'alias derrière elle.

Le cas qu'on veut reproduire est l'autre : un alias valide que le temps
a rattrapé, et dont la prochaine échéance tombe en impayé sans que le
porteur ait rien fait. On enrôle donc une carte saine, puis on la fait
vieillir :

```bash
curl -X POST http://localhost:30880/paysim/api/v1/payment-methods/{token}/expire
```

Idempotent, comme `revoke`. Tout `simulate` ou `charge_token` ultérieur
sur ce moyen refuse alors pour `moyen de paiement expire`, sans code
bancaire : c'est Paysim qui refuse, pas un émetteur.

Cette action est propre à Paysim et n'existe pas chez PayZen — elle vit
dans l'API de contrôle, jamais dans les routes du fournisseur.

**Sémantique d'expiration** (convention bancaire française) : une
carte est valide jusqu'au dernier jour du mois d'expiration inclus.
`expiryMonth: 8, expiryYear: 2026` reste valide durant tout août 2026
et refuse à partir du 1er septembre. `IsExpired` retourne true
uniquement quand le mois/année courant est strictement postérieur à
l'expiration.

### Révocation manuelle — refus après révocation

Appeler `POST /paysim/api/v1/payment-methods/{token}/revoke`.
L'endpoint est idempotent (204 sur un token inconnu). Tout `simulate`
ou `charge_token` référençant ce moyen de paiement refuse par la suite.

## Exemple d'usage

```bash
# Enrôler une carte qui échouera aux rejeux récurrents
curl -X POST http://paysim:8080/paysim/api/v1/payments \
  -H 'Content-Type: application/json' \
  -d '{
    "amount": 1000, "currency": "EUR", "orderId": "SUB-1",
    "formAction": "REGISTER_PAY",
    "card": {
      "pan": "4000000000000002",
      "expiryMonth": 12,
      "expiryYear": 2028,
      "holderName": "DUPONT JEAN",
      "country": "US",
      "productCategory": "DEBIT",
      "issuerName": "BANQUE DE TEST"
    }
  }'
# → {"uuid":"...","paymentMethodToken":"...","brand":"VISA"}
#   brand accompagne le token pour que le marchand enregistre le vrai
#   réseau du premier coup, au lieu de retomber sur une valeur par
#   défaut jusqu'au paiement récurrent suivant.

# Premier paiement : refusé aussi — le PAN magique est vérifié au
# simulate, pas seulement sur les rejeux récurrents
curl -X POST http://paysim:8080/paysim/api/v1/payments/UUID/simulate \
  -d '{"outcome":"PAID"}'

# Rejeu un mois plus tard : refus automatique via le PAN magique
curl -X POST http://paysim:8080/paysim/api/v1/payments \
  -H 'Content-Type: application/json' \
  -d '{
    "amount": 1000, "currency": "EUR", "orderId": "SUB-1-M2",
    "paymentMethodToken": "TOKEN_DE_ENROLL"
  }'
# → {"state":"declined", ...}
```

## Génération aléatoire côté client

Pour un script d'intégration qui mixe succès et échecs aléatoirement :

```javascript
// 90% de succès, 10% de refus au rejeu
const pansRefuses = [
  '4000000000000002',
  '5105105105105100',
  '2223000000000007',
  '378282000000008',
];
const pan = Math.random() < 0.9
  ? '4111' + chiffresAleatoires(12)   // n'importe quel autre PAN Visa, succès
  : pansRefuses[Math.floor(Math.random() * pansRefuses.length)];
```

Tout PAN qui n'est pas une des quatre valeurs réservées est accepté par
Paysim comme une carte normale, indépendamment de la validité Luhn —
Paysim ne rejette jamais un paiement uniquement sur l'échec Luhn (c'est
un simulateur).

## Multi-provider

Le champ `provider` de `POST /paysim/api/v1/payments` (et de chaque
endpoint générique) sélectionne l'adaptateur. L'omettre retombe sur
`payzen` par défaut — le serveur logge le fallback en niveau Debug
pour permettre de tracer les choix implicites dans un log CI dense.

`provider` explicite pour préparer l'avenir (la surface API restera
identique quand Stripe arrivera) :

```bash
# Explicite — comportement identique aujourd'hui, résilient aux
# futurs adaptateurs
curl -X POST http://paysim:8080/paysim/api/v1/payments \
  -H 'Content-Type: application/json' \
  -d '{
    "provider": "payzen",
    "amount": 1000, "currency": "EUR", "orderId": "O-1"
  }'

# À venir — même endpoint, provider différent
# curl -X POST http://paysim:8080/paysim/api/v1/payments \
#   -d '{"provider":"stripe","amount":1000,"currency":"EUR","orderId":"O-1"}'
```

Les marchands qui utilisent un SDK officiel PSP (client Lyra,
`stripe-php`, …) ne touchent jamais à cette API générique — ils
tapent les URLs natives du provider (`/api-payment/V4/*` pour PayZen,
`/v1/payment_intents` pour Stripe). Là, c'est l'URL qui fait le
discriminant, pas besoin de champ `provider`. L'API générique vise
les scénarios, l'UI, et les scripts d'intégration qui parlent
« Paysim » directement.

Détails dans [subscriptions.fr.md](subscriptions.fr.md#cross-provider).

## Reconnaître un moyen de paiement inexploitable

Une carte dont le refus tient au solde et non au statut reste
**enregistrée** — `4000000000000002` s'enrôle alors que tout débit
refusera. C'est ce qui permet de rejouer un scénario d'impayé dessus.
Mais elle ne doit pas ressembler à une carte valide, aussi
`GET /paysim/api/v1/payment-methods` porte-t-il un verdict sur chaque
entrée :

```json
{ "token": "609114…", "panMasked": "400000XXXXXX0002", "revoked": false,
  "usable": false, "unusableReason": "carte de test refusee" }
```

`usable` couvre les trois causes d'un coup — révocation, expiration, PAN
de refus — et `unusableReason` nomme celle qui s'applique.

Les deux champs sont **dérivés à la lecture, jamais stockés** : les trois
causes se déduisent de ce qui est déjà là, et un indicateur figé
deviendrait faux au premier changement de mois sur une carte qui arrive
à expiration.

À noter : **un paiement refusé ne crée aucun alias.** La carte disparaît
avec la tentative, et la réponse de création ne porte donc pas de
`paymentMethodToken` — il n'y en a pas. C'est la règle de la
plateforme : « l'alias ne sera pas créé si la demande d'autorisation ou
de renseignement est refusée ».

Une exception apparente, qui n'en est pas une : le PAN de provision
insuffisante s'enrôle à zéro euro. La vérification n'engage aucun
montant, donc n'interroge pas le solde — la carte est acceptée, et ne
refusera qu'au premier débit. C'est ce qui permet d'obtenir un
abonnement dont les échéances refusent pour ce motif, le montant d'une
échéance étant imposé par l'échéancier. Les motifs tenant au statut de
la carte — `43`, `05`, `57` — font au contraire échouer la vérification
elle-même.

## Rappel de sécurité

**Ne jamais stocker de vraies CB dans Paysim.** Le champ `pan` est
persisté en clair, sans aucun contrôle PCI-DSS, dans la table
`payment_methods` (mode SQLite) ou dans une simple map en mémoire (mode
mémoire). C'est volontaire : Paysim simule un PSP ; les vraies cartes
n'ont rien à faire ici.
