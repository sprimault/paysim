> [🇬🇧 English](testing-cards.md) · [🇫🇷 Français](testing-cards.fr.md)

# Cartes de test et scénarios de refus

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

| Marque               | Préfixe  | Longueur | PAN de test         |
| -------------------- | -------- | :------: | ------------------- |
| Visa                 | `400000` |    16    | `4000000000000002`  |
| Mastercard           | `510510` |    16    | `5105105105105100`  |
| Mastercard série 2   | `222300` |    16    | `2223000000000007`  |
| American Express     | `378282` |    15    | `378282000000008`   |

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
| Carte expirée       | au `simulate` **et** au `charge_token`   | Carte expirée présentée (fidèle au PSP réel)     |
| Révocation manuelle | au `simulate` **et** au `charge_token`   | Moyen de paiement supprimé après l'enrôlement    |

### Montant magique — refus au simulate

Tout montant se terminant par `01` (`1001`, `2001`, `12301`…) force
l'endpoint `simulate` à retourner `UNPAID`. Contrôle l'issue du parcours
navigateur côté marchand.

### PAN magique — refus sur tout paiement

Attacher une carte avec un des quatre PANs ci-dessus au premier
paiement. L'appel `simulate` refuse même quand l'`outcome` demandé
est `PAID` ; un `charge_token` ultérieur sur le token stocké refuse
également. Fonctionne indépendamment du montant et de tout état
préalable.

### Carte expirée — refus dès la présentation

Attacher une carte avec une date d'expiration passée (par exemple
`expiryMonth: 1, expiryYear: 2020`). Tout `simulate` ou `charge_token`
ultérieur refuse, conforme au comportement PSP réel : une carte est
refusée dès qu'elle est présentée, pas seulement au moment d'un
prélèvement récurrent.

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
      "expiryYear": 2028
    }
  }'
# → {"uuid":"...","paymentMethodToken":"..."}

# Premier paiement : simulate normalement (capture réussie)
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

## Rappel de sécurité

**Ne jamais stocker de vraies CB dans Paysim.** Le champ `pan` est
persisté en clair, sans aucun contrôle PCI-DSS, dans la table
`payment_methods` (mode SQLite) ou dans une simple map en mémoire (mode
mémoire). C'est volontaire : Paysim simule un PSP ; les vraies cartes
n'ont rien à faire ici.
