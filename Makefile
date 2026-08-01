.PHONY: dev test lint vulncheck sec build fixtures web-types web-build web-test web-lint

dev:
	@echo "dev: pas encore implémenté (phase 3)" && exit 1

# Les cibles Go dépendent de web-build : le paquet internal/webui
# embarque le bundle via //go:embed all:dist et refuse de compiler si
# internal/webui/dist/ est absent. On build le front une fois pour
# toutes les cibles Go — l'incrément Vite est rapide (~500ms) et
# garantit la cohérence.
test: web-build
	go test -race ./...

lint: web-build
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

fixtures:
	@echo "fixtures: pas encore implémenté (phase 2)" && exit 1

# web-types régénère les types TypeScript de l'API à partir des DTOs
# Go de internal/api via tygo. À relancer après tout changement de
# signature JSON exposée à l'UI.
web-types:
	@command -v tygo >/dev/null 2>&1 || { echo "tygo absent : go install github.com/gzuidhof/tygo@latest"; exit 1; }
	tygo generate --config tools/tygo/tygo.yaml

web-build:
	cd web && npm run build

web-test:
	cd web && npm run test:run

web-lint:
	cd web && npm run lint
