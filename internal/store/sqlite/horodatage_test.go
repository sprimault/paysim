// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package sqlite

import (
	"strings"
	"testing"
	"time"
)

// La convention de sérialisation des instants n'était écrite nulle part :
// elle se déduisait de dix-huit répétitions. Ces tests la fixent, pour
// qu'un dépôt ajouté plus tard ne puisse pas s'en écarter en silence.

func TestHorodaterEcritEnUTCAvecLesNanosecondes(t *testing.T) {
	t.Parallel()

	paris := time.FixedZone("CEST", 2*3600)
	cas := []struct {
		nom    string
		entree time.Time
		veut   string
	}{
		{
			// Le décalage doit disparaître au profit de l'UTC : deux
			// instants identiques écrits depuis deux fuseaux doivent
			// produire la même chaîne, sinon le tri les sépare.
			nom:    "converti en UTC",
			entree: time.Date(2026, 8, 22, 15, 30, 0, 0, paris),
			veut:   "2026-08-22T13:30:00Z",
		},
		{
			// La précision est ce qui départage deux écritures d'une même
			// milliseconde. La perdre rendrait le tri par updated_at
			// non déterministe.
			nom:    "nanosecondes conservées",
			entree: time.Date(2026, 8, 22, 13, 44, 13, 303018935, time.UTC),
			veut:   "2026-08-22T13:44:13.303018935Z",
		},
		{
			nom:    "instant sans fraction",
			entree: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			veut:   "2026-01-02T03:04:05Z",
		},
	}
	for _, c := range cas {
		t.Run(c.nom, func(t *testing.T) {
			t.Parallel()
			if got := horodater(c.entree); got != c.veut {
				t.Errorf("horodater = %q, veut %q", got, c.veut)
			}
		})
	}
}

// Un aller-retour ne doit rien perdre : c'est la propriété dont dépend
// tout tri chronologique lu depuis la base.
func TestAllerRetourPreserveLInstant(t *testing.T) {
	t.Parallel()

	instants := []time.Time{
		time.Date(2026, 8, 22, 13, 44, 13, 303018935, time.UTC),
		time.Date(2026, 2, 29, 23, 59, 59, 999999999, time.UTC), // année bissextile
		time.Date(2026, 1, 1, 0, 0, 0, 1, time.UTC),             // une nanoseconde
		time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),             // époque
	}
	for _, veut := range instants {
		got, err := lireHorodatage("test_at", horodater(veut))
		if err != nil {
			t.Fatalf("lireHorodatage(%s) : %v", veut, err)
		}
		if !got.Equal(veut) {
			t.Errorf("aller-retour = %s, veut %s", got, veut)
		}
	}
}

// Le nom de colonne dans l'erreur n'est pas décoratif : sur une base
// rafistolée à la main, il évite de relire les cinq dépôts pour savoir
// laquelle a échoué.
func TestLireHorodatageNommeLaColonneEnErreur(t *testing.T) {
	t.Parallel()

	_, err := lireHorodatage("completed_at", "pas une date")
	if err == nil {
		t.Fatal("une valeur illisible doit remonter une erreur")
	}
	if !strings.Contains(err.Error(), "completed_at") {
		t.Errorf("le message doit nommer la colonne : %v", err)
	}
	if !strings.Contains(err.Error(), "pas une date") {
		t.Errorf("le message doit citer la valeur fautive : %v", err)
	}
}
