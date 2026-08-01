// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package arch

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDomainDoesNotImportProviders est le test d'architecture central de la
// phase 0. Il vérifie que le paquet internal/domain n'importe aucun paquet
// fournisseur, direct ou transitif via un import du dossier internal/providers.
//
// C'est le mécanisme qui garantit l'invariant 1 (voir .claude/critical-rules.md) :
// aucun champ vads_*, aucun identifiant pi_xxx, aucun vocabulaire de PSP dans
// le domaine. Cet invariant rend le moteur de chaos uniforme et l'ajout d'un
// fournisseur mécanique. S'il casse, ce n'est pas le test qu'il faut ajuster,
// c'est la conception.
//
// L'analyse est faite via go/parser en mode ImportsOnly — pas de dépendance
// externe, pas d'appel à go list en sous-processus.
func TestDomainDoesNotImportProviders(t *testing.T) {
	t.Parallel()
	assertNoImportMatch(t, "../domain", "internal/providers")
}

// assertNoImportMatch parcourt tous les fichiers Go du dossier donné et
// vérifie qu'aucun import ne contient la sous-chaîne interdite. On travaille
// par sous-chaîne plutôt que par comparaison exacte pour attraper les
// éventuels sous-paquets (internal/providers/payzen, internal/providers/stripe…).
func assertNoImportMatch(t *testing.T, dir, forbiddenSubstring string) {
	t.Helper()
	// parser.ParseDir est déprécié depuis Go 1.25 (ignore les build
	// tags). Ici on veut juste vérifier les imports statiques — pas
	// d'impact des build tags sur ces vérifs d'architecture. On liste
	// les fichiers .go à la main et on parse chacun individuellement.
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("lecture de %s : %v", dir, err)
	}
	found := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("analyse de %s : %v", path, err)
		}
		found++
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(p, forbiddenSubstring) {
				t.Errorf("%s importe %q — interdit (%s)", path, p, forbiddenSubstring)
			}
		}
	}
	if found == 0 {
		t.Fatalf("aucun fichier .go trouvé dans %s", dir)
	}
}
