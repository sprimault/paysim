.PHONY: dev test lint vulncheck sec build fixtures

dev:
	@echo "dev: pas encore implémenté (phase 3)" && exit 1

test:
	go test -race ./...

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint absent"; exit 1; }
	golangci-lint run

# vulncheck utilise l'outil officiel de la Go Team. Il ne signale une CVE
# que si le code appelle effectivement la fonction affectée — beaucoup
# moins bruyant qu'un scan de dépendances brut.
vulncheck:
	@command -v govulncheck >/dev/null 2>&1 || { echo "govulncheck absent : go install golang.org/x/vuln/cmd/govulncheck@latest"; exit 1; }
	govulncheck ./...

# sec exécute l'analyseur statique de sécurité gosec (credentials en dur,
# crypto faible, path/command injection, TLS mal configuré…).
sec:
	@command -v gosec >/dev/null 2>&1 || { echo "gosec absent : go install github.com/securego/gosec/v2/cmd/gosec@latest"; exit 1; }
	gosec ./...

build:
	CGO_ENABLED=0 go build -trimpath -o paysim ./cmd/paysim

fixtures:
	@echo "fixtures: pas encore implémenté (phase 2)" && exit 1
