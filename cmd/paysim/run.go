// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/sprimault/paysim/internal/scenarios"
)

// Codes de sortie de la sous-commande `paysim run`. Convention CI : 0
// succès, 1 échec fonctionnel (scénario faux vs simulé), 2 échec
// technique qui empêche même d'évaluer le scénario. Distinguer les
// deux permet à un job CI de router différemment : bloquer le merge
// sur 1, alerter l'infra sur 2.
const (
	exitOK        = 0
	exitAssertion = 1
	exitExec      = 2
)

// runCommand implémente `paysim run [--verbose] <scenario.yml>`. Signature
// choisie pour être testable sans processus : args et env sont injectés,
// stdout/stderr aussi. Retourne le code de sortie.
//
// args doit être `os.Args[2:]` (le mot-clé « run » consommé par le
// dispatcher). env est typiquement `os.Getenv` mais peut être remplacé
// par une closure dans les tests.
func runCommand(ctx context.Context, args []string, env func(string) string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("paysim run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	verbose := fs.Bool("verbose", false, "afficher chaque etape des sa completion")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: paysim run [--verbose] <scenario.yml>")
		_, _ = fmt.Fprintln(stderr, "  PAYSIM_URL       URL de l'API de controle du Paysim distant (obligatoire)")
		_, _ = fmt.Fprintln(stderr, "  PAYSIM_API_TOKEN Bearer de l'API de controle si configure (optionnel)")
	}
	if err := fs.Parse(args); err != nil {
		// ContinueOnError + fs.Usage définie ci-dessus : l'aide a déjà
		// été imprimée par fs.Parse en cas de flag inconnu.
		return exitExec
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return exitExec
	}
	path := fs.Arg(0)

	url := env("PAYSIM_URL")
	if url == "" {
		_, _ = fmt.Fprintln(stderr, "paysim run: PAYSIM_URL non defini (URL de l'API de controle du Paysim distant)")
		return exitExec
	}
	token := env("PAYSIM_API_TOKEN")

	s, err := scenarios.LoadFile(path)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "paysim run: %v\n", err)
		return exitExec
	}

	client := scenarios.NewClient(url, token)
	runner := scenarios.NewRunner(client)
	report := runner.Run(ctx, s)

	printReport(stdout, report, *verbose)

	switch {
	case report.Err() == nil:
		return exitOK
	case errors.Is(report.Err(), scenarios.ErrAssertion):
		return exitAssertion
	default:
		return exitExec
	}
}

// printReport rend le rapport en français, format humain. Verbose
// imprime chaque étape dès qu'elle finit (utile en développement) ;
// mode compact liste juste les étapes en erreur (utile en CI où les
// logs sont massifs et on veut aller au défaut vite).
func printReport(w io.Writer, r *scenarios.Report, verbose bool) {
	dur := r.Duration().Round(time.Millisecond)

	if verbose {
		for _, s := range r.Steps {
			status := "OK"
			if s.Err != nil {
				status = "ECHEC: " + s.Err.Error()
			}
			_, _ = fmt.Fprintf(w, "  etape %d %-16s %v %s\n",
				s.Index, s.Action, s.Duration.Round(time.Millisecond), status)
		}
	}

	if r.Err() == nil {
		_, _ = fmt.Fprintf(w, "OK — %s: %d etapes en %v\n", r.Scenario, len(r.Steps), dur)
		return
	}
	_, _ = fmt.Fprintf(w, "ECHEC — %s: %d etapes en %v\n", r.Scenario, len(r.Steps), dur)
	if verbose {
		// Détail déjà imprimé plus haut, on n'ajoute rien.
		return
	}
	for _, s := range r.Steps {
		if s.Err != nil {
			_, _ = fmt.Fprintf(w, "  etape %d (%s): %v\n", s.Index, s.Action, s.Err)
		}
	}
}
