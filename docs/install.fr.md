> [🇬🇧 English](install.md) · [🇫🇷 Français](install.fr.md)

# Installation

Paysim s'empaquette comme MailHog ou OnlyOffice : un conteneur qu'on
ajoute à son stack, configuré par variables d'environnement, consommé
par les autres services. Ce document couvre Docker Compose, Kubernetes
(NodePort ou Ingress), et les points de configuration.

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
| `PAYSIM_PUBLIC_URL` | URL vue par le navigateur (host d'ingress, ou `http://<ip-noeud>:30880` pour NodePort). |
| `PAYSIM_CALLBACK_URL` | URL de callback par défaut pour les webhooks — utilisée comme fallback quand un paiement n'a pas de `returnUrl`. |
| `PAYSIM_BASE_PATH` | Préfixe quand Paysim est servi sous un sous-chemin (ex. `/paysim`). Vide quand servi à la racine. |
| `PAYSIM_API_TOKEN` (+ `_FILE`) | Bearer token qui protège l'API de contrôle. Vide = ouvert (mode local uniquement). |
| `PAYSIM_PAYZEN_HMAC_KEY` (+ `_FILE`) | Clé HMAC-SHA-256 pour signer les `kr-answer` PayZen. |
| `PAYSIM_MAX_PAYMENTS` | Plafond de rétention pour le stockage mémoire. Défaut 10000. |
| `PAYSIM_LOG_LEVEL` | Niveau de log (`debug`, `info`, `warn`, `error`). Défaut `info`. |
| `PAYSIM_STORE` | Backend de stockage : `memory` (défaut, sans état) ou `sqlite`. |
| `PAYSIM_SQLITE_PATH` | Chemin du fichier SQLite quand `PAYSIM_STORE=sqlite`. Défaut `/data/paysim.db`. Nécessite un volume writable. |
| `PAYSIM_CHAOS_LATENCY_MS` | Latence injectée sur chaque requête REST V4. `0` désactive. |
| `PAYSIM_CHAOS_ERROR_RATE` | Pourcentage de requêtes REST V4 renvoyant une 500. `0-100`. |

## Option 1 — Docker Compose

Le chemin le plus simple pour tester en local. Copier
[`deploy/compose.yml`](../deploy/compose.yml) dans votre stack
existant et fixer les variables d'environnement.

```bash
docker compose -f deploy/compose.yml up -d
```

Navigateur sur `http://localhost:8080/`.

Pour valider le parcours complet avec un marchand PHP qui reçoit et
vérifie les webhooks :

```bash
docker compose -f deploy/compose.yml -f deploy/compose.demo.yml up
```

Le marchand demo écoute sur le port 9000 et écrit les signatures
vérifiées dans `examples/php/retours.log`.

## Option 2 — Kubernetes en outil de dev interne (NodePort)

Fonctionne comme MailHog : un Service NodePort accessible depuis
n'importe quel host sur le réseau du cluster, à
`http://<ip-noeud>:30880`. Pas de DNS, pas de TLS, pas d'ingress.

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

Éditer [`deploy/k8s/kustomization.yaml`](../deploy/k8s/kustomization.yaml)
pour pointer sur l'image importée localement :

```yaml
images:
  - name: ghcr.io/sprimault/paysim
    newName: paysim
    newTag: local
```

### 3. Renseigner le Secret

Éditer [`deploy/k8s/secret.yaml`](../deploy/k8s/secret.yaml) avec la
clé HMAC :

```yaml
stringData:
  PAYSIM_PAYZEN_HMAC_KEY: "votre-cle-hmac-ici"
  PAYSIM_API_TOKEN: ""  # vide = ouvert
```

Pour la production, utiliser sealed-secrets, SOPS ou external-secrets
plutôt que de commiter le fichier en clair.

### 4. Appliquer

```bash
kubectl apply -k deploy/k8s/
kubectl -n paysim rollout status deployment/paysim
```

Accéder à l'UI sur `http://<n-importe-quelle-ip-noeud>:30880/`.

## Option 3 — Kubernetes derrière un ingress (domaine + TLS + auth)

Pour exposer Paysim sur un nom d'hôte public avec TLS et contrôle
d'accès optionnel, activer l'ingress :

### 1. Décommenter l'ingress dans kustomization

```yaml
resources:
  - namespace.yaml
  - configmap.yaml
  - secret.yaml
  - deployment.yaml
  - service.yaml
  - ingress.yaml    # activé
```

### 2. Éditer `deploy/k8s/ingress.yaml`

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
sortie phase 6 — d'ici là, le build local + import est le chemin
recommandé.

## Persistance SQLite (optionnelle)

Par défaut Paysim garde tout en mémoire — le rootFS en lecture seule
du Deployment reflète ce choix. Pour persister les paiements entre
redémarrages :

1. Fixer `PAYSIM_STORE=sqlite` et `PAYSIM_SQLITE_PATH=/data/paysim.db`
   dans le ConfigMap.
2. Ajouter un `PersistentVolumeClaim` monté sur `/data` dans le
   Deployment.
3. Passer `readOnlyRootFilesystem: false` (ou monter un emptyDir en
   subPath pour `/data` si le root doit rester read-only).

Un overlay Kustomize qui applique ces changements automatiquement
sera fourni dans `deploy/k8s/overlays/sqlite/` (à venir en 4.3.7,
placeholder de section en attendant).

## Dépannage

- **Pod bloqué en `ImagePullBackOff`** — l'image n'est pas dans le
  store local du cluster. Pour k3d, utiliser `k3d image import` ;
  pour kind, `kind load docker-image` ; pour k3s, `k3s ctr images
  import`.
- **NodePort inaccessible** — vérifier le firewall (port 30880 par
  défaut) et que l'IP du node est routable depuis votre poste.
- **`ImagePullBackOff` sur un registre privé** — il manque un
  `imagePullSecret` dans le namespace, ou il ne matche pas la
  référence d'image.

## Intégration marchand

Le dossier [`examples/php`](../examples/php/README.fr.md) contient un
marchand PHP complet qui crée un paiement, reçoit le webhook signé,
et vérifie la signature `kr-hash` — la référence canonique pour
toute intégration.
