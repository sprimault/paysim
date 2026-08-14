# Feuille de route

Chaque phase est livrable seule et a un critère de fin vérifiable. On ne commence pas la
suivante avant que le critère soit atteint.

**Phase en cours : 5** (phase 4 close le 2026-08-02, tag `v0.4.0`)

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

**Close le 2026-08-02**, tag `v0.4.0`. Tous les livrables prévus dans la « Fini quand »
sont atteints, plus un bloc d'améliorations UI non planifiées (feature subscriptions,
feature payment methods, DataTable partagé, mode sombre/clair, tabs providers, refetch
au mount, bannière auto-reload sur nouveau build) et la publication du script de seed
(`examples/seed-paysim.{sh,ps1}`).

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
- **4.4.6c-doc fait** (2026-08-02) — `docs/subscriptions.md` + `.fr.md`
  bilingues : vue d'ensemble, choix « pas de scheduler en fond »,
  cycle de vie, endpoints, conditions de refus (partagées avec
  charge_token), scénario YAML type, HTTP curl équivalent, section
  cross-provider. Section `Multi-provider` ajoutée à `testing-cards.md`
  + `.fr.md` avec exemple `provider: payzen` explicite, mention Stripe
  en commentaire pour phase 5, note sur défaut payzen et log Debug,
  distinction SDK natif (URL discriminante) vs API générique
  (champ `provider`).
- **4.4.4 fait** (2026-08-02) — 5 scénarios canoniques dans
  `examples/scenarios/` (one-shot capture/declined, recurring-token,
  subscription complète, subscription-with-decline), tous validés par un
  test loader dédié. `docs/scenarios.md` + `.fr.md` bilingues (format
  YAML, référence des 11 actions, état implicite currentUUID/Token/SubID,
  codes retour CLI, cross-provider). `examples/php/scenario-complete.php`
  ajouté comme référence exhaustive (customer complet, metadata,
  formAction REGISTER_PAY, notificationUrl, card, wallet, threeDSStatus,
  chaos struct, extraction paymentMethodToken + rejeu one-click) ;
  `scenario.php` original conservé pour l'onboarding.
- **4.4.8 fait** (2026-08-02) — `docs/providers/payzen.md` + `.fr.md`
  bilingues, ~450 lignes chacun : vue d'ensemble, table de couverture
  endpoints (5 simulés + liste explicite des non-simulés), détail par
  endpoint avec chaque champ request/response + type + requis +
  valeurs autorisées + spécificités Paysim, endpoints de contrôle
  `/paysim/simulate/*`, structure kr-answer complète avec sous-
  structures (KrTransaction/KrCardDetails/KrThreeDSResponse), outcomes,
  valeurs magiques (amount + PAN + chaos struct), codes d'erreur
  PAYSIM_*, signature kr-hash avec vecteur validé contre le SDK Java
  Lyra. Structure symétrique pour `docs/providers/stripe.md` en
  phase 5.
- **4.4.7a fait** (2026-08-02) — Backend listing endpoints + colonne provider.
  `api.Deps` gagne `SubscriptionRepo` + `PaymentMethodRepo` (optionnels,
  nil en mode mémoire). Nouvel endpoint `GET /paysim/api/v1/payment-methods`
  qui liste avec PAN masqué + revoked. `payzenSubscriptions()` désormais
  brancé sur `SubscriptionRepository.ByProvider("payzen")` (retournait
  `nil, nil` avant). Helper de test `setupWithSQLite` + 4 nouveaux tests.
  Colonne « Provider » dans PaymentList React côté onglet « Tous »
  uniquement (propriété `showProvider` optionnelle sur PaymentRow).
- **4.4.7b fait** (2026-08-02) — Mode sombre/clair toggle utilisateur.
  Tailwind passe de `darkMode: 'media'` à `'class'`. Toggle 3 états
  (light/system/dark) dans le Header via nouveau `ThemeToggle` shared.
  Hook `useTheme` gère persistance localStorage + listener
  prefers-color-scheme en mode `system`. Script inline dans
  `index.html` applique la classe `dark` avant le premier render pour
  éviter le flash. Favicon SVG inline (data URI) ajouté : icône
  éclair indigo, cohérent avec le logo Header. 16 nouveaux tests
  (`theme.test.ts` + `ThemeToggle.test.tsx`).
- **4.4.7c fait** (2026-08-02) — Feature UI subscriptions + `DataTable`
  shared + migration router data mode. `entities/subscription/` (api +
  store Zustand + hooks list/detail miroir de payment),
  `features/subscription-list/` (table dense, badge Actif/Annulé, tri
  par createdAt), `features/subscription-detail/` (toutes les
  métadonnées + boutons Trigger + Cancel). Nouveau composant
  `shared/ui/DataTable` générique (colonnes déclaratives, empty state,
  loading skeleton) — utilisable par sub, methods et éventuellement
  payments (refactor futur non urgent). Navigation ajoutée au Header
  (3 liens Paiements / Abonnements / Moyens de paiement avec NavLink
  actif). Routes migrées dans nouveau `web/src/app/router.tsx` en mode
  data (createBrowserRouter, react-router v6.4+ / v7) plutôt que
  Routes/Route déclaratif — cohérence avec Cadensio, prépare loaders
  futurs. `main.tsx` utilise RouterProvider. **Séparation des
  responsabilités** : `App.tsx` = layout root (Header + Outlet +
  ToastContainer + SSE), `router.tsx` = mapping URL → composant.
  **Bonus** :
  `basename: getBasePath()` passé au router, corrige un bug latent où
  les Link/NavLink absolus ne prenaient pas le préfixe ingress en
  compte. Types TS régénérés via `make web-types`. 5 nouveaux tests
  DataTable + 4 tests SubscriptionList.
- **4.4.7d fait** (2026-08-02) — Feature UI payment methods (dernière
  du 4.4.7). Nouvel endpoint backend `GET /paysim/api/v1/payment-methods/{token}`
  pour accès unitaire (bookmark/navigation directe). `entities/payment-method/`
  (api + store + hooks list/detail miroir de subscription).
  `features/payment-method-list/` (DataTable réutilisé, colonnes État/
  Provider/Marque/PAN/Expiration/Token/Créé). `features/payment-method-detail/`
  (métadonnées + bouton Révoquer avec confirm). Lien depuis
  SubscriptionDetail vers le détail du moyen (paymentMethodToken devient
  cliquable). Routes /payment-methods et /payment-methods/:token ajoutées.
  3 tests API + 4 tests PaymentMethodList. **DataTable prouvé** :
  3e usage sans refactor, factorisation en 7c validée.
- **4.4.7 tout entier** (a+b+c+d) fait le 2026-08-02.
- **4.4.2b fait** (2026-08-02) — `inject` fonctionnel : `SimulatePaymentRequest`
  enrichi côté API (`chaos` structuré + `deliveryDelayMs`), client scenarios
  a un `SimulateOpts` optionnel, runner porte `pendingChaos`/`pendingDelayMs`
  dans son state avec portée **one-shot** (consommé par le prochain simulate).
  4 modes reconnus : `duplicate`, `bad-signature`, `race`, `delay=NNN`. Mode
  inconnu → erreur explicite (pas de dégradation silencieuse). Nouveau
  scénario canonique `chaos-duplicate.yml`. 4 tests runner (mode connu,
  mode inconnu, portée one-shot, delay invalide). Docs `scenarios.md` +
  `.fr.md` documentent les 4 modes.
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
- **Matrice URL fait** (2026-08-02) — cinq scénarios (binaire local, marchand hôte +
  Paysim conteneur, tout Compose, K8s NodePort, K8s Ingress) dans `docs/install.md`
  + `.fr.md`, en tête juste après la table des variables. Rappel de l'invariant 7 et
  du override par `notificationUrl` dans le body `simulate`.

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
  Sans lui, un pod qui tourne une semaine sature sa mémoire. Fait.
- Persistance **SQLite optionnelle**, désactivée par défaut, activée par une variable
  dédiée qui pointe sur un fichier local. Un seul fichier, driver Go pur, aucun serveur
  externe. Sert à rejouer une session après redémarrage ; ne change ni l'invariant 8
  (une seule réplique) ni le contrat d'ajout de Paysim comme conteneur autonome. Fait,
  y compris la persistance des events bus pour le catch-up SSE post-restart et
  l'overlay Kustomize.
- Protection de l'API de contrôle par jeton Bearer, sur le modèle du `JWT_SECRET`.
  Inactive tant que la variable est vide, pour ne pas alourdir l'usage local. Fait.
- **`docs/install.md`**, avec dans cet ordre : binaire seul, `docker run`, `compose` à côté
  d'une application, K3s/Kubernetes. Puis le tableau complet des variables. Puis — c'est la
  section qui compte — **la matrice des deux URL selon le scénario** : tout en local, appli
  sur l'hôte et Paysim en conteneur (`host.docker.internal`), tout en `compose` (noms de
  services), cluster (nom de service interne et hôte d'ingress). C'est là que se posent
  toutes les questions des utilisateurs ; le reste de la doc n'est que du confort. Fait.

**Fini quand** : le même dépôt d'exemple passe ses tests en CI sans aucun identifiant, et
tourne à l'identique en `compose` et sur un cluster K3s en suivant `docs/install.md` sans
avoir à deviner quoi que ce soit.

---

## Fait — la famille Lyra

Systempay, Sogecommerce, Scellius et Lyra Collect sont couverts. Il n'y a rien eu à écrire :
la plateforme est unique et seul l'hôte distingue les marques, or l'hôte est ce que le
marchand fait pointer sur Paysim.

Établi par sondage des API de production plutôt que par lecture de documentation : 24
services existants et 12 inexistants répondent le même code d'erreur, chemin pour chemin,
sur les quatorze hôtes, avec un chemin inventé comme témoin. Détail et hôtes réels dans
`docs/providers/lyra-family.md`.

Cette entrée annonçait une surface de configuration à concevoir — plusieurs jeux
d'identifiants nommés. **Elle n'a pas lieu d'être** : Paysim ne valide aucun identifiant.
La contrainte reviendra avec Monetico, mais pour une autre raison — le numéro de TPE entre
dans le calcul du sceau, donc il compte réellement.

---

## Phase 5 — Horloge contrôlable

Le temps réel est appelé en direct 26 fois, dans huit paquets — dont `internal/domain`, qui
en devient non déterministe, et `internal/delivery`, où vit la temporisation exponentielle
des réessais. Une interface `Clock` existe déjà, mais dans `internal/providers/payzen` : le
domaine, qui n'importe jamais un fournisseur, ne peut pas s'en servir. La couture est
construite du côté qui n'en a pas besoin.

Sans horloge contrôlable, aucun scénario ne peut rien affirmer sur un réessai sans dormir.

Aucune dépendance extérieure — c'est ce qui la place devant les fournisseurs.

- Sortir `Clock` des adaptateurs vers un paquet neutre.
- Supprimer les appels directs à `time.Now()` hors du point d'injection.
- Exposer l'avance du temps dans l'API de contrôle et dans les scénarios.

**Fini quand** : un scénario canonique fait basculer un alias en expiré par une avance du
temps, sans toucher au dépôt ni dormir.

Le critère portait d'abord sur la deuxième tentative d'un webhook. Il était inatteignable :
`internal/delivery` ne réessaie pas — une tentative unique, les réessais étant annoncés
depuis la phase 2 sans avoir jamais été livrés. Ils forment leur propre phase, et l'horloge
est ce qui les rendra vérifiables.

---

## Phase 6 — Adoption

Le dépôt est public et l'image publiée depuis le 3 août 2026. Ce qui reste ne concerne plus
la mise à disposition, mais le fait qu'on la trouve, qu'on l'intègre et qu'on y contribue.

- [x] README avec la démonstration en premier écran et le renvoi vers `docs/install.md`.
- [ ] Documentation par fournisseur, avec la version d'API visée.
- [x] `CONTRIBUTING.md` publiant les invariants, la mise en route et ce qu'on attend d'une
  pull request — sans eux, un correctif propre se fait refuser sur une règle invisible.
- [ ] La recette d'ajout d'un fournisseur, à écrire quand la phase 7 aura extrait la
  couture : aujourd'hui `internal/api` dépend directement du paquet `payzen`.
- [ ] Annonce ciblée : communautés PHP/Symfony francophones d'abord, puis plus large.

---

## Phase 7 — Deuxième fournisseur : Stripe

Valide que l'abstraction tient. Si l'ajout oblige à modifier `internal/domain`, c'est que la
phase 1 a mal découpé — on corrige là, pas ici.

Placée avant Monetico pour une raison pratique : sa documentation est publique et complète,
donc l'adaptateur peut être écrit aux standards du projet sans attendre l'accès de
quiconque.

**La lacune est entière malgré l'outillage de l'éditeur.** `stripe-mock` est sans état : il
valide la forme des requêtes, ignore totalement leur contenu, et renvoie un succès là où on
attendait l'erreur voulue — Stripe le documente et recommande de tester contre le testmode.
La CLI rejoue des événements préfabriqués, mais rien ne fait courir le webhook contre la
réponse HTTP, ne le duplique ni ne le désordonne.

**Axe de protocole complémentaire** de PayZen : corps JSON au lieu de `form-urlencoded`,
signature dans un en-tête horodaté au lieu d'un champ du corps, clé d'idempotence. C'est
`internal/delivery` qui est mis à l'épreuve, lui qui ne sait construire qu'une seule forme
de corps.

La fenêtre de tolérance de la signature devient un cas de chaos testable grâce à la phase 5.

**Fini quand** : `stripe-php` fonctionne sans modification contre Paysim, et que
`internal/domain` n'a pas bougé.

---

## Phase 8 — Troisième fournisseur : Monetico

Le meilleur ratio douleur/effort du marché français. L'environnement de test est adossé à un
TPE configuré sur le compte : rien hors ligne, rien en CI. Et la documentation prévoit qu'on
signale par courriel les erreurs rencontrées sur l'URL de confirmation, avec un lien pour
rejouer la requête — le débogage de l'asynchrone y est artisanal. C'est le trou que Paysim
comble.

Deuxième axe de protocole : formulaire à sceau et redirection, comme Sips.

**Condition d'entrée, bloquante.** L'accès à une spécification faisant autorité passe par le
canal client — jeux de test, codes retour, cas limites du sceau. Or l'invariant du projet
interdit de fabriquer un vecteur de signature : sans captures réelles, l'adaptateur ne peut
pas être écrit aux standards du projet — pas « moins bien », pas du tout. La phase commence
par lever cet accès, et ne commence pas s'il ne l'est pas.

**Deux contrats.** Monetico distingue le TPE des commandes de celui des abonnements. La
surface de configuration doit donc porter plusieurs jeux d'identifiants par fournisseur —
contrainte déjà rencontrée par la famille Lyra, et c'est là qu'elle doit avoir été conçue.

**Fini quand** : une intégration Monetico réelle passe sans modification, et que
`internal/domain` n'a pas bougé.

---

## Plus tard

Idées valides mais hors périmètre. On les note ici plutôt que de les commencer.

- Fournisseurs supplémentaires : Sips/Worldline (Mercanet, Sogenactif), Mollie, Adyen.
- Lyra Inde — `api.in.lyra.com` n'expose pas REST V4 mais une API distincte : chemins
  `/pg/rest/v1/charge`, vocabulaire d'états `DUE`/`PAID`/`DROPPED`, enveloppe différente,
  webhook déclaré à la création de la charge. Exclusion vérifiée dans les deux sens. À
  traiter comme un fournisseur à part entière le jour où quelqu'un le demande, jamais
  comme une variante de marque.
- Signature à algorithme inattendu — le client JavaScript de la plateforme reposte tel quel
  le `kr-hash-algorithm` que le serveur lui donne, sans le valider ; la restriction à
  `sha256_hmac` vit uniquement dans le SDK marchand. Émettre autre chose est donc
  structurellement possible en vrai, et fait tomber une branche d'erreur que personne ne
  teste. Relève de `internal/chaos`.
- Réessais de livraison avec temporisation exponentielle. `internal/delivery` ne tente
  qu'une fois ; les réessais sont annoncés en commentaire depuis la phase 2 sans avoir
  jamais été écrits. Ils changent le comportement de livraison, donc leur propre validation
  et leurs propres notes de release. L'horloge de la phase 5 est ce qui les rendra
  vérifiables sans dormir.
- SEPA et R-transactions — la plus grosse lacune du marché : cycle de vie du mandat, rejets
  et retours qui tombent à J+3/J+5. Hors périmètre pour le moment. À reprendre en sachant
  que ça élargit délibérément le domaine, et que ça suppose l'horloge de la phase 5.
- Fusion avec Mailsio dans un conteneur unique de services de développement.
- Faux S3, capteur de webhooks génériques.
- Chart Helm, si les manifestes bruts deviennent pénibles à maintenir.
