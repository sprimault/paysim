// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFichierDeSecretVideRefuseLeDemarrage : un fichier vide donnait une
// valeur vide, qui désactive silencieusement la protection concernée —
// jeton d'API ou signature. L'instance démarrait, readyz passait au
// vert, et la surface restait ouverte à qui l'atteignait.
//
// Le cas n'est pas théorique : une clé de Secret renommée, ou un
// `kubectl create secret --from-file` sur un fichier vide, suffisent.
func TestFichierDeSecretVideRefuseLeDemarrage(t *testing.T) {
	t.Parallel()

	cas := []struct {
		nom     string
		contenu string
	}{
		{"totalement vide", ""},
		{"un saut de ligne", "\n"},
		{"des espaces et une tabulation", "  \t\r\n"},
	}
	for _, c := range cas {
		t.Run(c.nom, func(t *testing.T) {
			t.Parallel()
			chemin := filepath.Join(t.TempDir(), "secret")
			if err := os.WriteFile(chemin, []byte(c.contenu), 0o600); err != nil {
				t.Fatal(err)
			}
			env := mockEnv{"PAYSIM_API_TOKEN_FILE": chemin}

			_, err := secretValue(env.lookup, os.ReadFile, "PAYSIM_API_TOKEN")
			if err == nil {
				t.Fatal("un fichier de secret vide doit faire échouer la configuration, pas ouvrir l'API")
			}
			if !strings.Contains(err.Error(), "PAYSIM_API_TOKEN_FILE") {
				t.Errorf("le message doit nommer la variable fautive : %v", err)
			}
		})
	}
}

// TestVariableDirecteVideResteLeModeOuvert : le mode ouvert du
// développement local passe par la variable, pas par un fichier. Le
// resserrement ne doit pas l'emporter.
func TestVariableDirecteVideResteLeModeOuvert(t *testing.T) {
	t.Parallel()

	env := mockEnv{"PAYSIM_API_TOKEN": ""}
	got, err := secretValue(env.lookup, os.ReadFile, "PAYSIM_API_TOKEN")
	if err != nil {
		t.Fatalf("une variable vide reste licite : %v", err)
	}
	if got != "" {
		t.Errorf("valeur = %q, veut vide", got)
	}
}
