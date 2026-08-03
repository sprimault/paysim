# Paysim — plateforme de test de paiement

Faux prestataire de paiement, pour le développement et les tests d'intégration.

L'objectif n'est **pas** de simuler un paiement qui réussit — les sandbox le font déjà.
C'est de provoquer à la demande les échecs qu'une sandbox refuse de produire : webhook
arrivé avant la réponse HTTP, livraison en double ou dans le désordre, timeout au milieu
d'une capture, 3DS abandonné, impayé différé.

Toute décision de conception s'arbitre en faveur de cette phrase.

Les invariants du projet sont dans `.claude/critical-rules.md`, réinjecté automatiquement
à chaque session. Ils ne sont pas répétés ici.

## Commandes

Cibles standard du projet.

| Commande | Effet |
|---|---|
| `make dev` | Serveur en rechargement à chaud + front en HMR |
| `make test` | `go test -race ./...` |
| `make lint` | `golangci-lint run` |
| `make build` | Binaire unique, front embarqué |
| `make fixtures` | Régénère les fixtures depuis les captures de `testdata/raw/` |

## Architecture

```
cmd/paysim/            point d'entrée, câblage des dépendances
internal/
  domain/              Payment, machine à états, événements
  providers/           adaptateurs de protocole (payzen, puis stripe)
  chaos/               injection de pannes
  delivery/            file de livraison des webhooks
  scenarios/           chargement et exécution des scénarios YAML
  store/               persistance (mémoire circulaire, puis SQLite optionnel)
  api/                 API de contrôle consommée par le front et les tests
  format/              formatage partagé : date.go, text.go, number.go, money.go
web/
  src/lib/             équivalent front : dates.ts, strings.ts, numbers.ts
deploy/
  Dockerfile           multi-étapes, image finale minimale
  compose.yml          exemple à côté d'une application
  k8s/                 Deployment, Service, Ingress, ConfigMap, Secret
docs/
```

<!-- Cette arborescence est ici parce que le code n'existe pas encore. Dès que le domaine
     est livrée, elle devient déductible du dépôt : la supprimer et laisser Claude lire
     l'arborescence réelle. Passer /doctor pour vérifier ce qui peut encore être coupé. -->

Le découpage `domain` / `providers` / `delivery` n'est pas cosmétique : c'est lui qui rend
le moteur de chaos uniforme et l'ajout d'un fournisseur mécanique. Voir les invariants.

`internal/format` et `web/src/lib` existent dès le début, même presque vides. Un
formatage de date, de montant ou de troncature de chaîne écrit en ligne dans un composant
ou un handler est une erreur à corriger, pas un raccourci acceptable.

## Contrat de conteneur et de cluster

Paysim se déploie via `compose` ou dans un cluster : on l'ajoute, on lui passe des
variables, il fonctionne. Aucun fichier de configuration, aucun script d'amorçage.

### Configuration

Tout par variables d'environnement, préfixe `PAYSIM_`. Chaque variable portant un secret
accepte aussi son doublon suffixé `_FILE`, qui lit la valeur dans un fichier — c'est ce qui
permet de monter un Secret Kubernetes sans écrire la valeur en clair dans le manifeste.

| Variable | Rôle |
|---|---|
| `PAYSIM_PUBLIC_URL` | Ce que voit le navigateur : hôte d'ingress, ou `localhost:30880`. |
| `PAYSIM_CALLBACK_URL` | Cible des webhooks côté réseau interne : nom de service. |
| `PAYSIM_BASE_PATH` | Préfixe quand l'ingress sert Paysim sous un sous-chemin. |
| `PAYSIM_API_TOKEN` (+ `_FILE`) | Protège l'API de contrôle pour les appels serveur-à-serveur. Vide = ouvert, pour le local. Activer désactive l'UI web (SPA sans login) — utiliser une basic auth ingress pour protéger l'UI. |
| `PAYSIM_MAX_PAYMENTS` | Plafond de rétention en mémoire. |
| `PAYSIM_LOG_LEVEL` | Niveau de journalisation. |

### Exécution

- **Un seul port HTTP** pour tout : interface, API de contrôle, routes des fournisseurs.
- **Sans état par défaut**, système de fichiers en lecture seule, utilisateur non
  privilégié. La persistance SQLite reste optionnelle et explicite.
- **Journaux sur la sortie standard**, jamais dans un fichier.
- `GET /healthz` pour la sonde de vivacité, `GET /readyz` pour celle de disponibilité —
  deux points d'entrée distincts, pas un alias.
- **Arrêt propre sur SIGTERM**, dans cet ordre : `/readyz` échoue d'abord pour que le
  Service cesse de router, puis la file de livraison se vide, puis on sort.
- Image finale `scratch` ou `distroless`, multi-architecture amd64 et arm64.

### Une seule réplique

Contrainte structurante, voir les invariants : l'état vit en mémoire et n'est pas partagé.
Les manifestes sont en `replicas: 1` avec `strategy: Recreate`. Le jour où ce serait
gênant, la réponse est de sortir l'état dans `internal/store`, pas d'ajouter des répliques.

## Conventions de travail

- Commits conventionnels : préfixe imposé par la norme (`feat:`, `fix:`, `test:`, `docs:`,
  `refactor:`), description en français.
- Une branche et une PR par livraison de la feuille de route.
- Pour toute modification touchant `internal/domain` ou l'interface `Provider` : proposer
  le plan avant d'écrire du code.

## Où trouver le reste

- `ROADMAP.md` — les phases, leurs critères de fin, et la section « Plus tard » où se
  notent les idées hors périmètre. À lire quand on planifie, pas à chaque session.
- `docs/states.md` — la machine à états du paiement.
- `docs/install.md` — installation et configuration, tous modes de déploiement.
- `.claude/rules/preferences.md` — posture, langue, style de code. Chargé à chaque session.
- `.claude/rules/{go,providers,web}.md` — conventions techniques, chargées automatiquement
  quand Claude touche les fichiers concernés.
