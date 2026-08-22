module github.com/sprimault/paysim

// Aligné sur l'image de build du Dockerfile (golang:1.26-alpine). La CI
// installe Go depuis ce fichier : les laisser diverger faisait compiler
// le binaire publié par un toolchain qu'aucun check n'exerçait, alors
// que ce projet dépend de comportements de la bibliothèque standard —
// routage de ServeMux, défauts du client HTTP qui livre les webhooks,
// sémantique de Server.Shutdown.
go 1.26.0

require (
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.55.0
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sys v0.46.0 // indirect
	modernc.org/libc v1.74.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)
