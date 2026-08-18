> [🇬🇧 English](CONTRIBUTING.md) · [🇫🇷 Français](CONTRIBUTING.fr.md)

# Contribuer à Paysim

Les contributions sont bienvenues. Cette page existe pour qu'un correctif
bien écrit ne soit pas refusé au nom d'une règle que personne ne pouvait
connaître.

## Ce que cherche le projet

> L'objectif n'est **pas** de simuler un paiement qui réussit — les
> sandbox le font déjà. C'est de provoquer à la demande les échecs
> qu'une sandbox refuse de produire : webhook arrivé avant la réponse
> HTTP, livraison en double ou dans le désordre, timeout au milieu d'une
> capture, 3DS abandonné, impayé différé.

Toute décision de conception s'arbitre en faveur de cette phrase, et
toute pull request avec elle. Un changement qui rend le chemin nominal
plus agréable, plus rapide ou plus joli, sans donner au marchand une
nouvelle façon de casser, est hors sujet — aussi bon soit le code.

## Les règles non négociables

Elles précèdent toute contribution et ne se négocient pas dans une pull
request. Ce sont elles qui rendent le moteur de chaos uniforme et l'ajout
d'un fournisseur mécanique ; si l'une d'elles vous gêne, c'est de la
conception qu'on discute, dans une issue, avant d'écrire du code.

1. **`internal/domain` n'importe jamais `internal/providers`.** Aucun
   champ `vads_*`, aucun identifiant `pi_xxx`, aucun vocabulaire de
   fournisseur dans le domaine. `internal/arch/arch_test.go` analyse les
   imports et échoue si cela change — quand il casse, c'est la conception
   qui est en cause, pas le test.
2. **Tout webhook passe par `internal/delivery`.** Aucun envoi direct
   depuis un adaptateur, même sur le chemin nominal.
3. **On reproduit le protocole réel tel qu'il est, défauts compris.** On
   ne normalise pas, on ne corrige pas, on ne modernise pas. Un faux qui
   ment est pire que pas de faux.
4. **Aucun test ne sort sur le réseau.** Les vecteurs de signature
   viennent de captures réelles dans `testdata/`, jamais générés par
   notre propre code — un vecteur produit par l'implémentation qu'il est
   censé vérifier ne prouve rien. `cmd/paysim-record` est le proxy
   d'enregistrement qui les produit. Si un vecteur manque, le demander
   plutôt que le fabriquer.
5. **Le chaos n'est jamais actif par défaut.** Il s'active explicitement :
   configuration, scénario, ou valeur magique.
6. **Aucune dépendance externe nouvelle sans discussion** — Go comme npm.
   La bibliothèque standard est le défaut : `net/http` et son `ServeMux`,
   pas de framework web, pas d'ORM, pas de bibliothèque d'assertion. La
   question à laquelle répondre est « qu'est-ce que ça m'évite d'écrire »,
   pas « est-ce que c'est populaire ».
7. **URL publique et URL interne sont deux configurations distinctes.**
   L'une sert aux redirections navigateur, l'autre aux appels serveur à
   serveur. Ne jamais dériver l'une de l'autre ni retomber sur
   `localhost` : ça marche hors conteneur et casse dans tout `compose` et
   tout cluster.
8. **Paysim ne tourne qu'en un seul exemplaire.** Paiements, journal
   d'événements et file de livraison vivent en mémoire, sans partage.
   Deux répliques derrière un Service produisent des résultats
   incohérents sans jamais lever d'erreur.

## Mettre en route

Il faut Go (la version épinglée dans `go.mod`), Node 22, et une chaîne C
pour `go test -race`.

```bash
git clone https://github.com/sprimault/paysim.git
cd paysim
```

| Commande | Effet |
|---|---|
| `make dev` | Backend et front en HMR sur http://127.0.0.1:5173 |
| `make test` | `go test -race ./...` |
| `make lint` | `golangci-lint run` |
| `make build` | Binaire unique, front embarqué |
| `make web-types` | Régénère les types TypeScript depuis les structs Go |
| `make web-types-check` | Régénère et échoue si les types ont dérivé |

Passer par les cibles `make` plutôt que d'appeler `go test` directement :
elles construisent d'abord le front, que `internal/webui` embarque via
`//go:embed` et sans lequel la compilation Go échoue.

`make web-types` compte plus qu'il n'y paraît. Les types TypeScript de
`web/src/shared/model/` sont générés depuis les structs Go par tygo, et un
job de CI échoue dès qu'ils divergent. À lancer dès qu'on touche une
struct exportée ou une constante exportée que le front consomme —
**y compris pour reformuler un commentaire** : tygo recopie les godoc
dans le TypeScript produit.

`make lint` en dépend, donc la dérive se voit avant le push plutôt qu'une
fois la pull request ouverte. `make web-types-check` la lance seule.

## Ce qu'on attend d'une pull request

- **Une branche et une pull request par livraison.** Branche de base :
  `master`.
- **Commits conventionnels** : `feat:`, `fix:`, `test:`, `docs:`,
  `refactor:`. Le préfixe est imposé par la norme ; la description est de
  préférence en français, l'anglais est accepté — le préfixe est ce que
  lit l'outillage, la description est ce que lisent les humains.
- **Les tests accompagnent le changement.** Un comportement non couvert
  est un comportement qui régressera en silence.
- **Toutes les vérifications au vert.** Chaque pull request exécute le
  linter Go, les tests unitaires avec le détecteur de concurrence, le
  linter et les tests du front, un audit des dépendances, un contrôle de
  dérive des types générés, et les sept scénarios canoniques contre un
  vrai binaire dans les deux modes de stockage.
- **Le message de commit dit ce que fait le changement et pourquoi.** La
  validation et le contexte d'exécution vont dans la pull request, qui
  s'adresse au relecteur et non à qui lira l'historique dans deux ans.

Le formatage, les montants et les dates passent par les bibliothèques
partagées — `internal/format` côté Go, `web/src/shared/lib/` côté front. Les
montants sont des entiers en centimes, jamais des flottants, et leur
conversion vers l'affichage passe par `money.go` et nulle part ailleurs.

## Ajouter un fournisseur

Pas encore. PayZen est aujourd'hui le seul fournisseur, et la couture par
laquelle un deuxième s'insérerait n'est pas taillée : `internal/api`,
`cmd/paysim` et `internal/scenarios` dépendent encore directement du
paquet `payzen`. Il n'existe aujourd'hui aucune interface `Provider` à
implémenter.

Extraire cette couture est le travail de la phase du deuxième fournisseur, mené par le
mainteneur précisément parce que c'est le moment où l'abstraction se
conçoit au lieu de se deviner. Cette page décrira la recette quand la
recette existera. D'ici là, ouvrir une issue plutôt que de commencer un
adaptateur qu'il faudrait réécrire.

## Signaler un bogue

Les rapports les plus utiles sont ceux qui montrent que Paysim affirme
quelque chose de faux sans le signaler — un webhook décrivant une carte
qui n'a jamais été présentée, un état que la documentation promet et que
le binaire ne produit pas. Dire ce que vous avez fait, ce que vous
attendiez du vrai PSP, et ce que Paysim a répondu.

Les failles de sécurité font exception : elles passent par le canal privé
décrit dans [`SECURITY.fr.md`](SECURITY.fr.md), jamais par une issue
publique. Cette page dit aussi ce qui en est une — Paysim est permissif
par conception, et l'essentiel de cette permissivité est délibéré.
