# Feuille de route

Chaque phase est livrable seule et a un critère de fin vérifiable. On ne commence pas la
suivante avant que le critère soit atteint.

**Phase en cours : 0**

---

## Phase 0 — Le domaine, sans HTTP

Machine à états du paiement, en mémoire, testée. Aucun serveur, aucune interface, aucun
fournisseur.

- États : `initiated → authorized → captured → refunded / partially_refunded`, plus les
  branches `declined`, `expired`, `chargeback`.
- Transitions explicites, transitions interdites qui retournent une erreur typée.
- Journal d'événements immuable par paiement — c'est lui qui alimentera la chronologie.
- Le test d'architecture qui interdit `domain → providers`.
- `internal/format` amorcé, `money.go` en premier : les montants sont en centimes entiers
  dès la première ligne de code, c'est irrattrapable après coup.
- Le chargement de configuration `PAYSIM_*`, avec les deux URL distinctes en place tout de
  suite. Les rajouter plus tard obligerait à reprendre chaque adaptateur.
- Le `Makefile` et ses cibles.

**Fini quand** : `go test ./internal/domain/...` couvre toutes les transitions valides et
invalides, et que le diagramme d'états est dans `docs/states.md`.

---

## Phase 1 — Un fournisseur, en vrai : PayZen

L'adaptateur complet : formulaire de paiement, calcul et vérification de signature sur les
champs `vads_*`, retour navigateur, IPN serveur à serveur.

- L'interface `Provider` se dégage à partir de ce cas concret, pas avant.
- Vecteurs de signature capturés dans `testdata/`.
- Une application d'exemple minimale en PHP dans `examples/symfony/`, qui sert à la fois de
  validation réelle et de documentation.

**Fini quand** : l'application d'exemple effectue un paiement complet de bout en bout contre
Paysim, sans modification du SDK PayZen autre que l'URL de base — et le fait aussi bien en
local qu'avec les deux services dans un `compose`. Ce second cas est le seul qui valide
vraiment la séparation des deux URL.

---

## Phase 2 — Le chaos et le mode enregistrement

C'est le différenciant. Il arrive tôt, avant l'interface, pour valider la proposition de
valeur.

- Valeurs magiques : montant se terminant par `.01` → refus, telle carte → 3DS obligatoire,
  telle métadonnée → timeout.
- Injection de pannes : latence, 5xx, webhook en double, webhook dans le désordre,
  signature invalide, et surtout **webhook livré avant le retour de la réponse HTTP**.
- **Mode enregistrement** : Paysim se place en proxy devant la vraie sandbox, capture les
  échanges et en fait des fixtures. Ça règle le problème des vecteurs de signature, et ça
  rend l'ajout d'un fournisseur bien moins fastidieux.

**Fini quand** : les six modes de panne sont déclenchables et couverts par des tests, et que
la course webhook/réponse se provoque en trois lignes.

---

## Phase 3 — L'interface

React + TypeScript, embarquée dans le binaire.

- Chronologie des transactions, flux temps réel en SSE.
- Journal de livraison des webhooks avec bouton de rejeu.
- Boutons pour faire avancer un paiement dans sa machine à états.
- Vue requête/réponse côte à côte.

**Fini quand** : un GIF de trente secondes montre un paiement, un webhook rejoué et une
panne injectée. Ce GIF est l'actif principal du projet — il vaut plus que le README.

---

## Phase 4 — Scénarios, conteneur et cluster

- Définition de scénarios en YAML, commités dans le dépôt de l'utilisateur.
- `paysim run scenario.yml` avec un code de retour exploitable en CI.
- `deploy/Dockerfile` multi-étapes, image publiée en amd64 et arm64.
- `deploy/compose.yml` montrant les deux URL correctement renseignées — c'est l'exemple que
  les gens copieront sans le lire, il doit être juste.
- `deploy/k8s/` : Deployment en `replicas: 1` et `strategy: Recreate`, sondes sur
  `/healthz` et `/readyz`, ConfigMap pour les URL, Secret pour `PAYSIM_API_TOKEN`, Ingress.
- Plafond de rétention `PAYSIM_MAX_PAYMENTS`, avec un stockage conçu en tampon circulaire.
  Sans lui, un pod qui tourne une semaine sature sa mémoire — c'est le défaut de MailHog
  qu'on ne reproduit pas.
- Persistance **SQLite optionnelle**, désactivée par défaut, activée par une variable
  dédiée qui pointe sur un fichier local. Un seul fichier, driver Go pur, aucun serveur
  externe. Sert à rejouer une session après redémarrage ; ne change ni l'invariant 8
  (une seule réplique) ni le contrat d'ajout de Paysim comme conteneur autonome. À
  livrer seulement si le besoin est confirmé au terme de la phase 3 ; sinon reporté.
- Protection de l'API de contrôle par jeton, sur le modèle du `JWT_SECRET` d'OnlyOffice.
  Inactive tant que la variable est vide, pour ne pas alourdir l'usage local.
- **`docs/install.md`**, avec dans cet ordre : binaire seul, `docker run`, `compose` à côté
  d'une application, K3s/Kubernetes. Puis le tableau complet des variables. Puis — c'est la
  section qui compte — **la matrice des deux URL selon le scénario** : tout en local, appli
  sur l'hôte et Paysim en conteneur (`host.docker.internal`), tout en `compose` (noms de
  services), cluster (nom de service interne et hôte d'ingress). C'est là que se posent
  toutes les questions des utilisateurs ; le reste de la doc n'est que du confort.

**Fini quand** : le même dépôt d'exemple passe ses tests en CI sans aucun identifiant, et
tourne à l'identique en `compose` et sur un cluster K3s en suivant `docs/install.md` sans
avoir à deviner quoi que ce soit.

---

## Phase 5 — Deuxième fournisseur : Stripe

Sert autant à élargir l'audience qu'à valider que l'abstraction tient. Si l'ajout de Stripe
oblige à modifier `internal/domain`, c'est que la phase 1 a mal découpé — on corrige là, pas
ici.

**Fini quand** : `stripe-php` fonctionne sans modification contre Paysim, et que
`internal/domain` n'a pas bougé.

---

## Phase 6 — Sortie publique

- README avec le GIF en premier écran et le renvoi vers `docs/install.md`.
- Documentation par fournisseur, avec la version d'API visée.
- `CONTRIBUTING.md` expliquant comment ajouter un fournisseur — c'est la porte d'entrée des
  contributeurs.
- Annonce ciblée : communautés PHP/Symfony francophones d'abord, puis plus large.

---

## Plus tard

Idées valides mais hors périmètre. On les note ici plutôt que de les commencer.

- Fournisseurs supplémentaires : Ogone/Worldline, Mollie, Adyen.
- Fusion avec Mailsio dans un conteneur unique de services de développement.
- Faux S3, capteur de webhooks génériques.
- Chart Helm, si les manifestes bruts deviennent pénibles à maintenir.
