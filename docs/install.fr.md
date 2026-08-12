> [🇬🇧 English](install.md) · [🇫🇷 Français](install.fr.md)

# Installation

Paysim est distribué comme un conteneur unique qu'on ajoute à sa
stack, configuré par variables d'environnement, consommé par les
autres services. Ce document couvre Docker Compose, Kubernetes
(NodePort ou Ingress), et les points de configuration.

> **Exemples shell** : les blocs `bash` ci-dessous supposent Git Bash
> sous Windows ou un shell POSIX. Pour PowerShell natif, remplacer
> `VAR=value cmd` par `$env:VAR="value"; cmd`, et `curl` par
> `Invoke-RestMethod`. Les commandes Docker CLI sont identiques
> sur les deux.

## Prérequis

- **Docker** 20+ pour les builds locaux et Compose.
- **kubectl** 1.22+ pour la partie Kubernetes (matching de route
  par verbe HTTP, arrivé avec la lib Go 1.22 exposée par kubectl 1.22).
- Un cluster (k3s, k3d, kind, ou autre) pour la partie K8s.

## Configuration

Toute la configuration passe par des variables d'environnement
préfixées `PAYSIM_`. Chaque variable portant un secret accepte un
doublon suffixé `_FILE` qui lit la valeur depuis un fichier — utile
pour monter un Secret K8s sans écrire la valeur en clair.

| Variable | Rôle |
|---|---|
| `PAYSIM_PUBLIC_URL` | URL vue par le navigateur (host d'ingress, ou `http://<ip-noeud>:30890` pour NodePort). |
| `PAYSIM_CALLBACK_URL` | URL de callback par défaut pour les webhooks — utilisée comme fallback quand un paiement n'a pas de `returnUrl`. |
| `PAYSIM_BASE_PATH` | Préfixe quand Paysim est servi sous un sous-chemin (ex. `/paysim`). Vide quand servi à la racine. |
| `PAYSIM_API_TOKEN` (+ `_FILE`) | Jeton Bearer qui protège l'API de contrôle pour les **appels serveur-à-serveur** (CI, scripts, tests). **Désactive l'UI web** si défini — la SPA n'a pas de flow de login et n'injecte pas de Bearer dans ses fetch. Pour protéger l'UI dans un environnement partagé, utiliser une basic auth au niveau de l'ingress (voir [Option 3 — Ingress](#option-3--kubernetes-derrière-un-ingress-domaine--tls--auth)). Vide = ouvert. |
| `PAYSIM_PAYZEN_HMAC_KEY` (+ `_FILE`) | Clé HMAC-SHA-256 qui signe le **retour navigateur** (`kr-hash-key: sha256_hmac`). |
| `PAYSIM_PAYZEN_REST_PASSWORD` (+ `_FILE`) | Mot de passe d'API REST qui signe les **notifications serveur à serveur** (`kr-hash-key: password`). **Obligatoire** dès que la clé HMAC est définie — voir ci-dessous. |
| `PAYSIM_MAX_PAYMENTS` | Plafond de rétention pour le stockage mémoire. Défaut 10000. |
| `PAYSIM_LOG_LEVEL` | Niveau de log (`debug`, `info`, `warn`, `error`). Défaut `info`. |
| `PAYSIM_STORE` | Backend de stockage : `memory` (défaut, sans état) ou `sqlite`. |
| `PAYSIM_SQLITE_PATH` | Chemin du fichier SQLite quand `PAYSIM_STORE=sqlite`. Défaut `/data/paysim.db`. Nécessite un volume writable. |
| `PAYSIM_AUTOPLAY` | Joue chaque paiement dès sa création, sans attendre d'appel de simulation. Défaut `false`. Voir plus bas. |
| `PAYSIM_CHAOS_LATENCY_MS` | Latence injectée sur chaque requête REST V4. `0` désactive. |
| `PAYSIM_CHAOS_ERROR_RATE` | Pourcentage de requêtes REST V4 renvoyant une 500. `0-100`. |

## Les deux clés PayZen

PayZen ne signe pas ses deux canaux avec la même clé, et il annonce
laquelle il a employée dans le champ `kr-hash-key` du POST :

| `kr-hash-key` | Canal | Clé côté Paysim | Clé côté marchand |
| --- | --- | --- | --- |
| `sha256_hmac` | Retour navigateur | `PAYSIM_PAYZEN_HMAC_KEY` | clé HMAC de la boutique |
| `password` | Notification serveur (IPN) | `PAYSIM_PAYZEN_REST_PASSWORD` | mot de passe d'API REST |

C'est le SDK marchand qui choisit sa clé d'après ce champ — le SDK PHP
officiel le fait explicitement :

```php
if ($_POST['kr-hash-key'] == "sha256_hmac") { $key = $this->_hashKey; }
elseif ($_POST['kr-hash-key'] == "password") { $key = $this->_password; }
```

**Les deux variables sont donc obligatoires**, et Paysim refuse de
démarrer si la seconde manque. Tout signer avec la clé du navigateur
« marcherait » ici, mais laisserait la branche `password` du code
marchand inexercée jusqu'à sa mise en production — où son IPN échouerait
alors qu'il passait en test. C'est le genre de faux qu'il vaut mieux ne
pas avoir.

Les deux valeurs peuvent être identiques en local ; ce qui compte est
que le marchand emprunte le bon chemin de vérification.

## `PAYSIM_AUTOPLAY` — tests de bout en bout sans porteur

En production, rien ne se passe après un `CreatePayment` tant que le
porteur ne s'est pas authentifié sur le formulaire et que le PSP n'a pas
notifié l'issue. Paysim reproduit fidèlement ce comportement : un
paiement neuf reste `initiated` tant que personne ne l'a joué, ce que
fait un appel de simulation.

Un test de bout en bout automatisé n'a personne pour tenir ce rôle. Le
contournement tentant — appeler l'API de simulation depuis le code du
marchand — est un mauvais échange : il fait entrer la mécanique du
simulateur dans le code métier et valide un enchaînement qui n'existera
jamais en production. Il ne prouve donc rien, et devient du code mort le
jour de la bascule vers le vrai PSP.

`PAYSIM_AUTOPLAY=true` joue l'acte côté serveur à la place. Aucun client
ne pilote quoi que ce soit, le contrat REST reste intact, et un paiement
est capturé — ou refusé — dès sa création, notification comprise.

**L'issue reste dictée par les valeurs magiques.** Un montant magique,
un PAN de refus ou une carte expirée décident exactement comme
d'habitude : ce mode automatise *qui joue*, pas *ce qui sort*. Tous les
leviers de [testing-cards.fr.md](testing-cards.fr.md) continuent de
fonctionner à l'identique.

**Ce à quoi il renonce** : `ABANDONED` et `EXPIRED` supposent un porteur
qui n'aboutit jamais. Avec l'autoplay, aucun paiement ne reste en
suspens — ces issues exigent donc un appel de simulation explicite, et
par conséquent ce mode désactivé. C'est la raison pour laquelle il est
opt-in, et n'a rien à faire ailleurs que dans un environnement de test.

Le serveur logue un `WARN` au démarrage quand il est actif.

## Matrice des deux URL

Paysim manipule deux URL indépendantes :

- **`PAYSIM_PUBLIC_URL`** — URL par laquelle un **navigateur** atteint
  Paysim (redirections, liens absolus rendus dans l'UI).
- **`PAYSIM_CALLBACK_URL`** — URL par défaut vers laquelle **Paysim**
  livre les webhooks quand un paiement n'a pas de `notificationUrl`
  explicite. C'est la vue **réseau interne** du marchand, depuis le
  pod Paysim.

**Elles ne se dérivent pas l'une de l'autre** (invariant 7). Deviner
l'une à partir de l'autre marche en local mais casse dans tout
Compose et tout cluster. L'en-tête `Host` d'une requête entrante
n'aide pas non plus : derrière un ingress, il ment.

| Scénario | `PAYSIM_PUBLIC_URL` | `PAYSIM_CALLBACK_URL` |
|---|---|---|
| Binaire local seul (dev) | `http://localhost:8080` | `http://localhost:<port-marchand>` |
| Marchand sur l'hôte, Paysim en conteneur | `http://localhost:30880` | `http://host.docker.internal:<port-marchand>` |
| Marchand + Paysim dans le même Compose | `http://localhost:30880` | `http://<service-marchand>:<port-interne>` (DNS de service) |
| Kubernetes NodePort | `http://<ip-noeud>:30890` | `http://<svc-marchand>.<ns>.svc.cluster.local:<port>` |
| Kubernetes derrière Ingress | `https://paysim.example.com` | `http://<svc-marchand>.<ns>.svc.cluster.local:<port>` |

**Cas non couvert** : un paiement peut inclure son propre
`notificationUrl` dans le body du `simulate` — il prime sur
`PAYSIM_CALLBACK_URL`. Recommandé pour un test CI qui envoie le
webhook vers un mock local sans dépendre d'une variable
d'environnement partagée.

## Option 1 — Docker Compose

Le chemin le plus simple pour tester en local. Copier
[`deploy/compose.yml`](../deploy/compose.yml) dans votre stack
existant et fixer les variables d'environnement.

```bash
docker compose -f deploy/compose.yml up -d
```

Navigateur sur `http://localhost:30880/`.

Surcharger `PAYSIM_HOST_PORT` si `30880` est pris — `PAYSIM_PUBLIC_URL`
suit automatiquement :

```bash
PAYSIM_HOST_PORT=30890 docker compose -f deploy/compose.yml up -d
# désormais http://localhost:30890/
```

Pour valider le parcours complet avec un marchand PHP qui reçoit et
vérifie les webhooks :

```bash
docker compose -f deploy/compose.yml -f deploy/compose.demo.yml up
```

Le marchand demo écoute sur le port 9000 et écrit les signatures
vérifiées dans `examples/php/retours.log`.

## Option 2 — Kubernetes en outil de dev interne (NodePort)

Un Service NodePort accessible depuis
n'importe quel host sur le réseau du cluster, à
`http://<ip-noeud>:30890`. Pas de DNS, pas de TLS, pas d'ingress.
(Le port `30880` reste libre pour le défaut Docker Compose, les deux
peuvent coexister sur la même machine.)

### 1. Builder l'image et la charger dans le cluster

Pour un cluster **k3d** local (Docker-in-Docker), l'image peut être
importée directement sans passer par un registre :

```bash
# Builder l'image OCI
docker build -f deploy/Dockerfile -t paysim:local .

# Importer dans le cluster k3d (remplacer 'mycluster' par le nom réel)
k3d image import paysim:local -c mycluster
```

Pour **k3s** ou **kind**, adapter :

```bash
# k3s : ctr sur chaque noeud
docker save paysim:local | sudo k3s ctr images import -

# kind
kind load docker-image paysim:local --name mycluster
```

Pour pulle depuis un registre au lieu du build local, voir la
section « Depuis un registre » ci-dessous.

### 2. Ajuster la référence d'image

Éditer [`deploy/k8s/base/kustomization.yaml`](../deploy/k8s/base/kustomization.yaml)
pour pointer sur l'image importée localement :

```yaml
images:
  - name: ghcr.io/sprimault/paysim
    newName: paysim
    newTag: local
```

### 3. Renseigner le Secret

Éditer [`deploy/k8s/base/secret.yaml`](../deploy/k8s/base/secret.yaml)
avec les deux clés PayZen :

```yaml
stringData:
  PAYSIM_PAYZEN_HMAC_KEY: "votre-cle-hmac-ici"
  PAYSIM_PAYZEN_REST_PASSWORD: "votre-mot-de-passe-rest-ici"
  PAYSIM_API_TOKEN: ""  # vide = ouvert
```

Les deux sont obligatoires : le pod refuse de démarrer si la seconde
manque.

Pour la production, utiliser sealed-secrets, SOPS ou external-secrets
plutôt que de commiter le fichier en clair.

### 4. Appliquer

```bash
kubectl apply -k deploy/k8s/
kubectl -n paysim rollout status deployment/paysim
```

Accéder à l'UI sur `http://<n-importe-quelle-ip-noeud>:30890/`.

## Option 3 — Kubernetes derrière un ingress (domaine + TLS + auth)

Pour exposer Paysim sur un nom d'hôte public avec TLS et contrôle
d'accès optionnel, activer l'ingress :

### 1. Décommenter l'ingress dans kustomization

Éditer [`deploy/k8s/base/kustomization.yaml`](../deploy/k8s/base/kustomization.yaml) :

```yaml
resources:
  - namespace.yaml
  - configmap.yaml
  - secret.yaml
  - deployment.yaml
  - service.yaml
  - ingress.yaml    # activé
```

### 2. Éditer `deploy/k8s/base/ingress.yaml`

Fixer le hostname et décommenter les annotations utiles :

**TLS via cert-manager** (nécessite un cluster-issuer opérationnel) :

```yaml
metadata:
  annotations:
    cert-manager.io/cluster-issuer: "letsencrypt-prod"
spec:
  tls:
    - hosts:
        - paysim.example.com
      secretName: paysim-tls
```

**Basic auth (nginx-ingress)** — protéger l'UI par login :

```bash
# Générer un fichier htpasswd
htpasswd -cB auth admin
# Créer un Secret à partir de ce fichier
kubectl -n paysim create secret generic paysim-basic-auth --from-file=auth
```

```yaml
metadata:
  annotations:
    nginx.ingress.kubernetes.io/auth-type: basic
    nginx.ingress.kubernetes.io/auth-secret: paysim-basic-auth
    nginx.ingress.kubernetes.io/auth-realm: "Paysim - authentification requise"
```

**Allow-list IP** :

```yaml
metadata:
  annotations:
    nginx.ingress.kubernetes.io/whitelist-source-range: "10.0.0.0/8,192.168.0.0/16"
```

### 3. Appliquer

```bash
kubectl apply -k deploy/k8s/
```

## Depuis un registre (optionnel)

Au lieu du build local + import, l'image peut être poussée sur un
registre et tirée depuis le cluster. Le `kustomization.yaml` par
défaut référence `ghcr.io/sprimault/paysim:latest` — à remplacer par
votre propre chemin.

**Build et push (multi-arch)** :

```bash
make image-push  # nécessite docker login et buildx configuré
```

**Registre privé** : créer un `imagePullSecret` dans le namespace
et le référencer depuis le Deployment.

Les images publiques poussées par CI sur GHCR arriveront à la
sortie publique — d'ici là, le build local + import est le chemin
recommandé.

## Persistance SQLite (optionnelle)

Par défaut Paysim garde tout en mémoire — le rootFS en lecture seule
du Deployment reflète ce choix. Pour persister les paiements,
l'historique des webhooks et les events du bus entre redémarrages,
appliquer l'overlay SQLite :

```bash
kubectl apply -k deploy/k8s/overlays/sqlite/
kubectl -n paysim rollout status deployment/paysim
```

L'overlay ajoute :

- un `PersistentVolumeClaim` nommé `paysim-data` (1Gi, RWO) monté
  sur `/data`. Ajuster la taille dans
  [`deploy/k8s/overlays/sqlite/pvc.yaml`](../deploy/k8s/overlays/sqlite/pvc.yaml)
  avant application si besoin ;
- `PAYSIM_STORE=sqlite` injecté comme variable d'env (le chemin par
  défaut étant `/data/paysim.db`, pas besoin de fixer explicitement
  `PAYSIM_SQLITE_PATH`).

`readOnlyRootFilesystem` reste `true` : seul `/data` est en écriture
via le volume monté. Le reste du filesystem conteneur demeure
immuable — même posture de sécurité que le mode mémoire par défaut.

L'invariant réplique unique est préservé (un PVC RWO ne peut être
lié qu'à un pod à la fois, et l'état en mémoire de Paysim ne serait
de toute façon pas cohérent entre plusieurs répliques).

## Dépannage

- **Pod bloqué en `ImagePullBackOff`** — l'image n'est pas dans le
  store local du cluster. Pour k3d, utiliser `k3d image import` ;
  pour kind, `kind load docker-image` ; pour k3s, `k3s ctr images
  import`.
- **NodePort inaccessible** — vérifier le firewall (port 30890 par
  défaut) et que l'IP du node est routable depuis votre poste.
- **`ImagePullBackOff` sur un registre privé** — il manque un
  `imagePullSecret` dans le namespace, ou il ne matche pas la
  référence d'image.

## Peupler un jeu de démo

Une fois Paysim démarré, remplir la base avec un jeu varié permet de
voir les états de l'UI (captured / declined / actif / révoqué /
expiré) sans écrire de curl à la main. Utile pour une première prise
en main :

Le défaut `PAYSIM_URL` est `http://localhost:30880` — fonctionne
d'emblée pour Docker Compose comme pour Kubernetes NodePort.
Surcharger `PAYSIM_URL` seulement si Paysim est déployé sur un autre
port ou une autre machine.

```bash
# Bash / Linux / macOS / Windows git-bash
bash examples/seed-paysim.sh
```

```powershell
# PowerShell natif sous Windows
./examples/seed-paysim.ps1
```

Les deux scripts — et les deux variantes de `demo-ui` plus bas —
produisent le même jeu, en deux parties.

Des cas remarquables d'abord, chacun porteur d'une chose à voir : un
enrôlement au contexte client complet, une capture nominale, trois
refus de motifs distincts (51, 43, 91), un paiement en attente, une
autorisation seule, un moyen expiré, un moyen révoqué, un abonnement à
deux échéances jouées, un dont l'échéance est refusée, un annulé, un
sans aucune échéance, et un rejeu one-click.

Des rejeux enfin, en nombres différents sur trois paiements — un sur
`CMD-1042`, deux sur `CMD-1047`, trois sur `CMD-2012`. C'est la
pastille du bouton de renvoi qui les compte, et sans eux elle ne
s'afficherait nulle part.

Du volume ensuite : trente paiements répartis sur les états. Cette
partie n'est pas décorative — la recherche, les filtres d'état, la
pagination et l'en-tête collant du tableau ne se jugent pas sur huit
lignes. Les états sont attribués par le rang plutôt qu'au hasard, si
bien que deux exécutions donnent le même écran.

À noter : l'échéance refusée vient d'un moyen révoqué *après* création
de l'abonnement. Enrôler une carte de test refusée ne marcherait pas —
une autorisation déclinée ne crée aucun alias, il n'y aurait donc rien
à abonner.

### Tout monter d'une commande

`seed-paysim.sh` suppose un Paysim déjà démarré. Pour ne rien avoir à
lancer soi-même, `examples/demo-ui.sh` monte l'ensemble — Paysim, un
récepteur de webhooks, le réseau Docker qui les relie — puis le peuple :

```bash
# Bash / Linux / macOS / Windows git-bash
bash examples/demo-ui.sh
```

```powershell
# PowerShell natif sous Windows
./examples/demo-ui.ps1
```

**Docker doit être installé et démarré** — sous Windows ou macOS, cela
veut dire Docker Desktop lancé, pas seulement installé. Les deux scripts
le vérifient d'emblée et s'arrêtent avec un message clair sinon.

Au-delà de Docker, la version bash demande `curl` et `grep` ; la
version PowerShell ne demande rien de plus, `Invoke-RestMethod` étant
natif. Les deux affichent à la fin l'URL de l'interface.

Le port se change par `PORT` / `-Port`, l'image par `IMAGE` / `-Image`
(`:edge` pour l'état de la branche principale). Sur un système où
`hostname -I` n'existe pas — macOS, notamment — renseigner `HOST_IP`
avec l'adresse par laquelle le navigateur joint la machine ; sous
PowerShell, `-HostIp` vaut `localhost` par défaut.

Tout se démonte d'une commande, rappelée en fin d'exécution :

```bash
docker rm -f paysim-demo paysim-sink && docker network rm paysim-demo-net
```

Le jeu de données y est choisi pour ce qui se regarde à l'écran : un
contexte client complet, trois refus de motifs différents, un paiement
resté en attente, un moyen inexploitable, un abonnement avec une
échéance jouée.

Options : `--purge` (bash) / `-Purge` (PowerShell) vide les paiements
existants avant de peupler. Définir `NOTIF_URL` pour livrer les
webhooks ailleurs (défaut : `http://localhost:1/discard` — port
fermé, échec immédiat, garde la vue webhooks lisible).

## Intégration marchand

Le dossier [`examples/php`](../examples/php/README.fr.md) contient un
marchand PHP complet qui crée un paiement, reçoit le webhook signé,
et vérifie la signature `kr-hash` — la référence canonique pour
toute intégration.
