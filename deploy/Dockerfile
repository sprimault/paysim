# syntax=docker/dockerfile:1.7

# ─── Stage 1 : build du front Vite ────────────────────────────────────
# Isolé pour bénéficier du cache npm sur les couches — un changement
# dans web/src/ ne réinvalide pas node_modules.
#
# Épinglé sur BUILDPLATFORM : le bundle est du JS et du CSS, identique
# quelle que soit l'architecture visée. Sans ça, un build multi-arch le
# regénère une fois par cible, la seconde sous émulation.
FROM --platform=$BUILDPLATFORM node:22-alpine AS webbuild
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY web/ ./
# Le bundle sort dans /web/../internal/webui/dist (outDir vite.config.ts,
# résolu depuis /web → /internal/webui/dist). On crée le rep parent
# pour que Vite puisse y écrire, sans symlink.
RUN mkdir -p ../internal/webui/dist && npm run build

# ─── Stage 2 : build du binaire Go ────────────────────────────────────
# CGO_ENABLED=0 → binaire statique posable dans distroless static.
# trimpath → chemins reproductibles, pas d'info sensible sur l'origine.
#
# Le stage tourne nativement (BUILDPLATFORM) et c'est GOARCH qui vise la
# cible : sans CGO, Go croise sans toolchain externe. Émuler ce stage
# reviendrait à faire tourner un compilateur sous QEMU pour produire ce
# qu'il sait générer directement — et imposerait binfmt sur toute
# machine de build.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS gobuild
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
# Le paquet internal/webui embed dist/ via //go:embed — il refuse de
# compiler si le rep est absent. On récupère le bundle du stage 1.
COPY --from=webbuild /internal/webui/dist /src/internal/webui/dist
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" \
    -o /paysim ./cmd/paysim

# Prépare un /data appartenant à nonroot (65532). Sans ça, un volume
# Docker/K8s monté sur /data hérite de root:root et SQLite ne peut pas
# écrire depuis distroless (pas de shell pour chown au runtime).
RUN mkdir -p /data-init && chown 65532:65532 /data-init

# ─── Stage 3 : runtime distroless nonroot ─────────────────────────────
# static-debian12 : glibc-free, ~2 MB de base, CA certs inclus (pour
# webhook HTTPS sortant), utilisateur nonroot (65532) par défaut.
# Aucun shell, aucun package manager — surface d'attaque quasi nulle.
#
# Volontairement sans --platform : c'est le seul stage qui doit suivre
# la cible, pour que BuildKit tire la base de chaque architecture.
FROM gcr.io/distroless/static-debian12:nonroot

# Commit d'où sort l'image, à passer par --build-arg : `.dockerignore`
# exclut `.git/`, le Dockerfile ne peut donc pas le déduire lui-même.
#
# C'est l'information qui manque le plus sur `edge`, reconstruite à
# chaque fusion : sans elle, deux images tirées à deux jours d'écart ne
# se distinguent que par leur digest, qui ne renvoie à rien. Reste
# `unknown` pour un build local, ce qui est le cas honnête — la valeur
# n'est fiable que si la chaîne de build la fournit.
ARG REVISION=unknown

# Rattache l'image au dépôt. GHCR lit ce label au push : sans lui, le
# package reste orphelin, et un workflow ne peut pas y écrire même avec
# packages:write — les droits d'un package sont distincts de ceux du
# dépôt tant que le lien n'est pas établi. C'est ce qui a fait échouer la
# première publication de l'image edge.
#
# Effet visible en prime : la page du package affiche le dépôt, son
# README et sa licence.
LABEL org.opencontainers.image.source="https://github.com/sprimault/paysim"
LABEL org.opencontainers.image.licenses="Apache-2.0"
LABEL org.opencontainers.image.description="Faux prestataire de paiement pour tests d'integration : provoque a la demande les echecs qu'une sandbox refuse de produire."
LABEL org.opencontainers.image.revision="${REVISION}"

COPY --from=gobuild /paysim /paysim
COPY --from=gobuild --chown=nonroot:nonroot /data-init /data
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/paysim"]
