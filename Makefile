.PHONY: dev test lint vulncheck sec build web-types web-types-check web-build web-test web-lint image image-push

# Variables de développement, toutes surchargeables à l'appel :
#   make dev PAYSIM_DEV_CALLBACK_URL=http://127.0.0.1:4000
#
# PUBLIC_URL pointe sur Vite et non sur le backend : c'est l'URL que
# voit le navigateur, et en dev c'est le serveur HMR qui l'accueille.
PAYSIM_DEV_PUBLIC_URL   ?= http://127.0.0.1:5173
PAYSIM_DEV_CALLBACK_URL ?= http://127.0.0.1:9099
PAYSIM_DEV_HMAC_KEY     ?= dev-hmac-key

# dev lance le backend Go et le serveur HMR de Vite côte à côte, puis
# les arrête ensemble. On ouvre http://127.0.0.1:5173 : Vite sert le
# front et relaie /paysim/* vers le backend sur :8080 (proxy déclaré
# dans web/vite.config.ts).
#
# Dépend de web-build parce que internal/webui embarque dist/ via
# //go:embed et refuse de compiler s'il est absent — un clone frais ne
# l'a jamais. Le bundle produit ici ne sert qu'à satisfaire le
# compilateur : c'est Vite qui sert le front pendant la session.
#
# Le rechargement à chaud du Go passe par wgo, mais son absence ne
# bloque pas : sans lui on perd le rechargement, pas la commande. Sur
# un dépôt public, `make dev` doit fonctionner sur un clone frais sans
# installer quoi que ce soit d'abord.
dev: web-build
	@trap 'kill 0' EXIT INT TERM; \
	(cd web && npm run dev) & \
	if command -v wgo >/dev/null 2>&1; then \
		PAYSIM_PUBLIC_URL=$(PAYSIM_DEV_PUBLIC_URL) \
		PAYSIM_CALLBACK_URL=$(PAYSIM_DEV_CALLBACK_URL) \
		PAYSIM_PAYZEN_HMAC_KEY=$(PAYSIM_DEV_HMAC_KEY) \
		wgo run ./cmd/paysim; \
	else \
		echo "wgo absent : pas de rechargement automatique du backend"; \
		echo "  go install github.com/bokwoon95/wgo@latest"; \
		PAYSIM_PUBLIC_URL=$(PAYSIM_DEV_PUBLIC_URL) \
		PAYSIM_CALLBACK_URL=$(PAYSIM_DEV_CALLBACK_URL) \
		PAYSIM_PAYZEN_HMAC_KEY=$(PAYSIM_DEV_HMAC_KEY) \
		go run ./cmd/paysim; \
	fi

# Les cibles Go dépendent de web-build : le paquet internal/webui
# embarque le bundle via //go:embed all:dist et refuse de compiler si
# internal/webui/dist/ est absent. On build le front une fois pour
# toutes les cibles Go — l'incrément Vite est rapide (~500ms) et
# garantit la cohérence.
test: web-build
	go test -race ./...

# La vérification des types générés est accrochée à lint, pas à test :
# `make test` doit rester exécutable sur un clone frais sans rien
# installer, alors que lint exige déjà golangci-lint et échoue sans lui.
# C'est aussi la cible qu'on lance avant chaque publication.
lint: web-build web-types-check
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint absent"; exit 1; }
	golangci-lint run

# vulncheck utilise l'outil officiel de la Go Team. Il ne signale une CVE
# que si le code appelle effectivement la fonction affectée — beaucoup
# moins bruyant qu'un scan de dépendances brut.
vulncheck: web-build
	@command -v govulncheck >/dev/null 2>&1 || { echo "govulncheck absent : go install golang.org/x/vuln/cmd/govulncheck@latest"; exit 1; }
	govulncheck ./...

# sec exécute l'analyseur statique de sécurité gosec (credentials en dur,
# crypto faible, path/command injection, TLS mal configuré…).
sec: web-build
	@command -v gosec >/dev/null 2>&1 || { echo "gosec absent : go install github.com/securego/gosec/v2/cmd/gosec@latest"; exit 1; }
	gosec ./...

build: web-build
	CGO_ENABLED=0 go build -trimpath -o paysim ./cmd/paysim

# web-types régénère les types TypeScript de l'API à partir des DTOs
# Go de internal/api via tygo. À relancer après tout changement de
# signature JSON exposée à l'UI — le job « Types générés » de la CI
# refuse la fusion si on l'oublie, parce qu'on l'a oublié trois fois.
#
# Version épinglée, et la CI installe la même : deux versions de tygo
# qui ne mettent pas les commentaires au même endroit feraient échouer
# ce contrôle sans qu'aucun type Go n'ait bougé. En changer ici oblige
# à changer TYGO_VERSION dans .github/workflows/ci.yml.
TYGO_VERSION ?= v0.2.21

web-types:
	@command -v tygo >/dev/null 2>&1 || { echo "tygo absent : go install github.com/gzuidhof/tygo@$(TYGO_VERSION)"; exit 1; }
	tygo generate --config tools/tygo/tygo.yaml

# TYPES_GENERES est la liste produite par tygo, déclarée une fois pour
# que le contrôle ci-dessous ne puisse pas en oublier un.
TYPES_GENERES = web/src/shared/model/api.ts web/src/shared/model/payzen.ts

# web-types-check régénère et refuse toute dérive. Même contrôle que la
# CI, à la même source — le job l'appelle plutôt que de refaire un diff
# en YAML, et les deux ne peuvent donc plus diverger.
#
# La comparaison porte sur les fichiers d'avant régénération, pas sur
# git : sur un poste, `make lint` tourne au milieu d'un travail en
# cours, et un diff contre HEAD échouerait sur des modifications
# légitimes et non encore commitées — c'est-à-dire exactement quand on
# vient de toucher un DTO.
#
# --strip-trailing-cr : sur un poste Windows, git repose les fichiers
# en CRLF à chaque bascule de branche alors que tygo écrit en LF. Sans
# ce drapeau, le premier `make lint` après un changement de branche
# échoue sur des fins de ligne, ce qui apprend surtout à ignorer le
# contrôle.
#
# Les fichiers régénérés restent en place : c'est le résultat à
# commiter, et les restaurer masquerait la correction qu'on vient de
# rendre nécessaire.
#
# Le contrôle n'existait que dans la CI, ce qui le faisait découvrir
# après le push, PR déjà ouverte, alors que git.md demande de vérifier
# avant de publier. Cinq fois en trois semaines, dont une pour une
# simple reformulation de commentaire — tygo recopie les godoc dans le
# TypeScript, et un texte reformulé suffit à faire dériver le fichier.
web-types-check:
	@command -v tygo >/dev/null 2>&1 || { echo "tygo absent : go install github.com/gzuidhof/tygo@$(TYGO_VERSION)"; exit 1; }
	@tmp=$$(mktemp -d) && cp $(TYPES_GENERES) "$$tmp/" && \
	tygo generate --config tools/tygo/tygo.yaml >/dev/null && \
	ecart=0; for f in $(TYPES_GENERES); do \
		diff -u --strip-trailing-cr "$$tmp/$$(basename $$f)" "$$f" || ecart=1; \
	done; rm -rf "$$tmp"; \
	if [ $$ecart -ne 0 ]; then \
		echo "Les types generes avaient derive des DTOs Go."; \
		echo "Le resultat est deja regenere : relire et commiter."; \
		exit 1; \
	fi; \
	echo "Types generes conformes aux DTOs Go"

web-build:
	cd web && npm run build

web-test:
	cd web && npm run audit:high && npm run test:run

web-lint:
	cd web && npm run lint

# ── Image OCI multi-arch ────────────────────────────────────────────
# Nécessite Docker Buildx et un builder buildkit actif (typiquement
# `docker buildx create --use --name paysim-builder` une fois pour
# toutes). L'image est construite pour amd64 et arm64 en un seul appel.
#
# `make image`      → construit sans publier, pour vérifier que le
#                     Dockerfile passe. Ne charge rien localement : une
#                     image multi-plateforme n'est pas chargeable, et le
#                     pilote distant laisse le résultat dans son cache.
# `make image-push` → build + push vers ghcr.io (nécessite `docker login ghcr.io`)
#
# Publier une version : une seule construction, deux étiquettes.
#
#	make image-push IMAGE_TAGS="ghcr.io/sprimault/paysim:v0.6.8 \
#	                            ghcr.io/sprimault/paysim:latest"
#
# En deux appels, la même source produit deux index différents — les
# horodatages de couches ne sont pas reproductibles. Repointer ensuite
# l'un sur l'autre laisse un index sans étiquette au registre, qu'il
# faut aller supprimer à la main. Arrivé à la v0.6.7.
IMAGE_TAG  ?= ghcr.io/sprimault/paysim:latest
IMAGE_TAGS ?= $(IMAGE_TAG)
TAG_FLAGS   = $(foreach t,$(IMAGE_TAGS),-t $(t))
PLATFORMS  ?= linux/amd64,linux/arm64
# Le contexte de build n'embarque pas .git/ : la révision se lit ici et
# se passe au Dockerfile. Le repli couvre le build depuis une archive.
REVISION ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)

image:
	docker buildx build --platform $(PLATFORMS) \
		--build-arg REVISION=$(REVISION) \
		$(TAG_FLAGS) -f deploy/Dockerfile .

image-push:
	docker buildx build --platform $(PLATFORMS) --push \
		--build-arg REVISION=$(REVISION) \
		$(TAG_FLAGS) -f deploy/Dockerfile .
