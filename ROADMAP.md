# Feuille de route

Chaque phase est livrable seule et a un critère de fin vérifiable. On ne commence pas la
suivante avant que le critère soit atteint.

**Phase en cours : 4**

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

## Phase 1 — Un fournisseur, en vrai : PayZen V4 (API REST)

L'adaptateur complet côté serveur : les endpoints REST V4 en HTTP Basic
(`CreatePayment`, `UpdatePayment`, `Transaction/Get`, `CreateSubscription`,
`Subscription/Get`), le retour navigateur et le webhook IPN signés en HMAC-SHA-256
hex (`kr-hash`) sur le body JSON `kr-answer`.

L'API Formulaire V2 (champs `vads_*` + redirection navigateur) reste hors périmètre :
elle est en fin de vie et n'est pas ce que visent les intégrations modernes. Le
SmartForm JavaScript côté client (SDK Krypton `KR` chargé depuis `static.payzen.eu`)
n'est pas simulé non plus — c'est un SDK officiel PayZen, on l'utilise tel quel dans
l'exemple d'intégration.

- L'interface `Provider` se dégage à partir de ce cas concret, pas avant.
- Vecteurs `kr-hash` capturés dans `testdata/`.
- Une application d'exemple minimale en PHP dans `examples/symfony/`, qui sert à la
  fois de validation réelle et de documentation.

**Fini quand** : l'application d'exemple effectue un paiement complet de bout en bout
contre Paysim — appel de `/V4/Charge/CreatePayment`, réception d'un `formToken`,
réception d'un retour navigateur signé et vérification du `kr-hash` — sans modification
du code d'intégration autre que l'URL de base. Le fait aussi bien en local qu'avec les
deux services dans un `compose`. Ce second cas est le seul qui valide vraiment la
séparation des deux URL.

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

État au 2026-08-02 : conteneur, cluster (avec overlay Kustomize SQLite optionnel validé
end-to-end sur k3d), plafond de rétention en ring buffer, protection API par jeton,
`docs/install.md` bilingue, **loader YAML des scénarios** (`internal/scenarios`, format
impératif à discriminant `action:`, six actions, validation agrégée), **moteur
d'exécution** (client HTTP + runner + endpoint générique `POST /paysim/api/v1/payments`
cross-provider) et **CLI `paysim run scenario.yml`** (dispatch args dans `cmd/paysim`,
config `PAYSIM_URL` + `PAYSIM_API_TOKEN`, codes retour 0/1/2, flag `--verbose`) —
faits. Restent : support token pattern + fausse CB stockée, subscriptions natives,
scénarios canoniques d'exemple, `docs/scenarios.md` bilingue, et la matrice des deux
URL dans `docs/install.md`.

Découpage restant de la phase 4 :

- **4.4.3** — fait : dispatch `paysim run <file>` dans `cmd/paysim`, `runCommand`
  testable (args + env + writers injectés), classification erreurs via nouvelle
  sentinelle `scenarios.ErrAssertion` pour distinguer exit 1 (assertion) vs 2
  (exécution).
- **4.4.5** — Token pattern PayZen + fausse CB stockée avec vérification d'expiration.
  Enrôlement via `formAction: REGISTER_PAY`, `paymentMethodToken` retourné dans le
  webhook et réutilisable dans un `CreatePayment` suivant sans formulaire. Store des
  moyens de paiement avec `pan_masked`, `expiry_month`, `expiry_year`, `revoked`.
  Vérification d'expiration au rejeu avec code `EXPIRED_CARD`. Endpoint de contrôle
  `POST /paysim/api/v1/payment-methods/{token}/revoke`. Actions YAML : `create_payment`
  enrichi (`register: true`, `card: {pan, expiry_month, expiry_year, brand}`) et
  `charge_token`. Aucune validation Luhn (simulateur), pas de whitelist PAN, clock
  injectable pour tests déterministes. **Rappel : n'utilisez jamais ce store avec de
  vraies CB, aucune protection.**
- **4.4.6a-serveur fait** (2026-08-02) — Endpoints API subscriptions :
  POST/GET /paysim/api/v1/subscriptions[/{id}], POST .../trigger-billing,
  POST .../cancel. `payzen.Handler.CreateSubscription` publique (miroir
  de `Create`), `TriggerBilling` qui crée une Transaction via
  `decideReplayOutcome` (même mécanique que charge_token), `Cancel`
  idempotent. `Subscription.Cancelled` + migration SQLite ADD COLUMN.
  Lien Transaction ↔ Subscription via `Metadata["subscriptionId"]`
  (Q2a). Log Debug quand provider vide → payzen défaut. 12 tests API.
- **4.4.6b-scenarios fait** (2026-08-02) — Actions YAML
  `create_subscription`, `trigger_billing`, `assert_subscription`,
  `cancel_subscription`. Runner mémorise `state.currentSubID` (miroir
  de currentUUID/currentToken). Défauts implicites : token vide →
  currentToken, subscription_id vide → currentSubID. Client HTTP
  enrichi. Fake serveur étendu. 6 tests bout-en-bout +
  `testdata/subscription.yml` canonique. UI fix : le placeholder
  "Aucun paiement" retire la mention PayZen (générique cross-provider).
- **4.4.6c** — Doc dédiée `docs/subscriptions.md` bilingue + exemples
  multi-provider dans testing-cards.md.
- **4.4.4** — Scénarios canoniques d'exemple (one-shot, token pattern, subscription)
  + `docs/scenarios.md` bilingue. Livré en dernier pour couvrir les trois patterns
  d'un seul jet.
- **4.4.7 — Extensions UI** — la mécanique 4.4.5/4.4.6 crée des entités que l'UI
  actuelle n'affiche pas. À ajouter : colonne « Provider » dans l'onglet « Tous » de
  la liste des paiements (aujourd'hui provider invisible en vue cross-provider) ;
  nouvelles vues liste + détail pour les subscriptions ; nouvelles vues pour les
  moyens de paiement enregistrés (avec PAN masqué, brand, expiration, révocation
  depuis l'UI). Prérequis : endpoint `GET /paysim/api/v1/payment-methods` (à ajouter
  au passage), `GET /paysim/api/v1/subscriptions` déjà là (6a). **Mode sombre/clair
  à prévoir** : sélecteur utilisateur, détection `prefers-color-scheme`,
  persistance localStorage. Tailwind supporte nativement (`dark:` variant).
- **4.4.2b** (mineur, à programmer) — Rendre l'action `inject` fonctionnelle : le
  runner accepte l'action mais retourne actuellement `errInjectUnsupported`. Enrichir
  `SimulatePaymentRequest` avec un champ `chaos` structuré (`WebhookChaos`) et
  logique runner qui mémorise le mode entre étapes. ~50 lignes.
- **4.4.5a-store fait** (2026-08-02) — Refactor `internal/store/` pour héberger
  `SubscriptionRepository` et `PaymentMethodRepository` génériques cross-provider
  (tables SQLite dédiées, migrations, tests). Extension `payzen.Store` interface
  avec `SaveMethod`/`MethodByToken`/`RevokeMethod`, converters PayZen ⇄ records,
  wiring `cmd/paysim` pour instancier les trois repos. Ferme le stub v1 mémoire
  des subscriptions dans SQLiteStore. Types `PaymentMethod`, `Card`, `Clock`,
  `NewPaymentMethod`, `maskPAN`, `BrandFromBIN`, `IsLuhnValid` dans
  `internal/providers/payzen/method.go`.
- **4.4.5c-scenarios fait** (2026-08-02) — Enrichissement du DSL YAML :
  `create_payment` gagne `card`/`form_action`/`notification_url` (Card structuré
  avec `pan`/`expiry_month`/`expiry_year`/`brand`), nouvelle action
  `charge_token` pour le rejeu one-click. Le runner mémorise le
  `paymentMethodToken` retourné dans `state.currentToken` (miroir de
  `currentUUID`), `charge_token` sans token explicite utilise ce dernier.
  Client HTTP enrichi (`ChargeToken`, `RevokePaymentMethod`), validation
  loader étendue, fake serveur de test reproduit les 3 flows serveur
  (nominal, enrôlement, rejeu avec refus déclenché par magic PAN /
  expiration / révocation). `testdata/recurring.yml` comme exemple.
- **4.4.5b-serveur fait** (2026-08-02) — Enrichissement `payzen.Handler.Create()`
  pour supporter les trois flows : nominal (existant), enrôlement (Card +
  `formAction: REGISTER_PAY|ASK_REGISTER_PAY` → génération token + attachement
  Transaction), rejeu one-click (`paymentMethodToken` → capture directe ou refus
  synchrone selon `decideReplayOutcome` : révocation → expiration → magic PAN →
  magic amount → PAID). Le webhook IPN est émis en fin de rejeu si
  `NotificationURL` + HMACKey présents. Enrichissement `CreatePaymentRequest`
  natif et `api.CreatePaymentInput` avec `Card` + `paymentMethodToken`. Nouvel
  endpoint `POST /paysim/api/v1/payment-methods/{token}/revoke`. Liste fermée
  de 4 PANs de refus (Visa/Mastercard/MC2/Amex) dans `internal/chaos` avec
  helper `IsDeclinedTestPAN`, valeurs Luhn-valides construites à partir des
  préfixes standards par marque. `docs/testing-cards.md` + `.fr.md` bilingue
  documente les 4 leviers (magic amount, magic PAN, expiration, révocation)
  avec exemples curl et JS. Codes d'erreur `PAYSIM_PAYMENT_METHOD_UNKNOWN`,
  `PAYSIM_EXPIRED_CARD`, `PAYSIM_REVOKED_CARD` ajoutés. 8 tests API +
  propagation `PaymentMethodToken` dans `KrTransaction` et
  `payzenProviderData` pour la persistance.
- **Matrice URL** dans `docs/install.md` (local, host+conteneur, compose, cluster).

- Définition de scénarios en YAML, commités dans le dépôt de l'utilisateur — fait pour
  le format et le loader (4.4.1) + moteur d'exécution (4.4.2).
- `paysim run scenario.yml` avec un code de retour exploitable en CI — voir 4.4.3.
- `deploy/Dockerfile` multi-étapes, image publiée en amd64 et arm64 — fait.
- `deploy/compose.yml` montrant les deux URL correctement renseignées — c'est l'exemple que
  les gens copieront sans le lire, il doit être juste. Fait.
- `deploy/k8s/` : Deployment en `replicas: 1` et `strategy: Recreate`, sondes sur
  `/healthz` et `/readyz`, ConfigMap pour les URL, Secret pour `PAYSIM_API_TOKEN`, Ingress.
  Fait (base + overlay SQLite).
- Plafond de rétention `PAYSIM_MAX_PAYMENTS`, avec un stockage conçu en tampon circulaire.
  Sans lui, un pod qui tourne une semaine sature sa mémoire — c'est le défaut de MailHog
  qu'on ne reproduit pas. Fait.
- Persistance **SQLite optionnelle**, désactivée par défaut, activée par une variable
  dédiée qui pointe sur un fichier local. Un seul fichier, driver Go pur, aucun serveur
  externe. Sert à rejouer une session après redémarrage ; ne change ni l'invariant 8
  (une seule réplique) ni le contrat d'ajout de Paysim comme conteneur autonome. Fait,
  y compris la persistance des events bus pour le catch-up SSE post-restart et
  l'overlay Kustomize.
- Protection de l'API de contrôle par jeton, sur le modèle du `JWT_SECRET` d'OnlyOffice.
  Inactive tant que la variable est vide, pour ne pas alourdir l'usage local. Fait.
- **`docs/install.md`**, avec dans cet ordre : binaire seul, `docker run`, `compose` à côté
  d'une application, K3s/Kubernetes. Puis le tableau complet des variables. Puis — c'est la
  section qui compte — **la matrice des deux URL selon le scénario** : tout en local, appli
  sur l'hôte et Paysim en conteneur (`host.docker.internal`), tout en `compose` (noms de
  services), cluster (nom de service interne et hôte d'ingress). C'est là que se posent
  toutes les questions des utilisateurs ; le reste de la doc n'est que du confort. Base
  bilingue faite ; la matrice URL reste à ajouter.

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
