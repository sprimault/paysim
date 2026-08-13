// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package clock

import (
	"sync"
	"testing"
	"time"
)

func TestSystem_enUTC(t *testing.T) {
	t.Parallel()
	got := System{}.Now()
	if got.Location() != time.UTC {
		t.Errorf("Location = %v, veut UTC", got.Location())
	}
}

func TestControllable_neutreALaConstruction(t *testing.T) {
	t.Parallel()
	c := NewControllable()
	if got := c.Offset(); got != 0 {
		t.Errorf("Offset initial = %v, veut 0", got)
	}
	// La capacité existe mais ne fait rien : l'écart à l'heure réelle
	// doit rester dans le bruit d'exécution, pas dans l'ordre de la
	// seconde.
	if ecart := c.Now().Sub(time.Now().UTC()); ecart > time.Second || ecart < -time.Second {
		t.Errorf("ecart a l'heure reelle = %v, veut ~0", ecart)
	}
}

func TestControllable_lesAvancesSeCumulent(t *testing.T) {
	t.Parallel()
	c := NewControllable()
	avant := c.Now()
	c.Advance(24 * time.Hour)
	c.Advance(24 * time.Hour)

	if got := c.Offset(); got != 48*time.Hour {
		t.Errorf("Offset = %v, veut 48h", got)
	}
	// Deux jours plus loin, à la marge d'exécution près.
	saut := c.Now().Sub(avant)
	if saut < 48*time.Hour || saut > 48*time.Hour+time.Second {
		t.Errorf("saut = %v, veut ~48h", saut)
	}
}

// TestControllable_leTempsContinueDAvancer couvre la raison d'être du
// décalage plutôt que de l'instant figé : deux lectures successives ne
// doivent pas rendre le même instant, sans quoi le tri par date de
// modification du dépôt mémoire dégénère en tri par UUID.
func TestControllable_leTempsContinueDAvancer(t *testing.T) {
	t.Parallel()
	c := NewControllable()
	c.Advance(72 * time.Hour)
	a := c.Now()
	time.Sleep(2 * time.Millisecond)
	b := c.Now()
	if !b.After(a) {
		t.Errorf("deux lectures rendent %v puis %v : le temps ne s'ecoule plus", a, b)
	}
}

func TestControllable_resetRevientALHeureReelle(t *testing.T) {
	t.Parallel()
	c := NewControllable()
	c.Advance(96 * time.Hour)
	c.Reset()
	if got := c.Offset(); got != 0 {
		t.Errorf("Offset apres Reset = %v, veut 0", got)
	}
	if ecart := c.Now().Sub(time.Now().UTC()); ecart > time.Second || ecart < -time.Second {
		t.Errorf("ecart apres Reset = %v, veut ~0", ecart)
	}
}

func TestControllable_avanceNegative(t *testing.T) {
	t.Parallel()
	c := NewControllable()
	c.Advance(48 * time.Hour)
	c.Advance(-24 * time.Hour)
	// Le paquet n'interdit rien : la garde qui refuse un recul vit dans
	// l'API, là où l'utilisateur formule sa demande. Ici on vérifie
	// seulement que l'arithmétique est celle qu'on croit.
	if got := c.Offset(); got != 24*time.Hour {
		t.Errorf("Offset = %v, veut 24h", got)
	}
}

// TestControllable_concurrence sert surtout sous -race : l'horloge est
// lue par tous les paquets et écrite par un endpoint HTTP, donc depuis
// des goroutines différentes.
func TestControllable_concurrence(t *testing.T) {
	t.Parallel()
	c := NewControllable()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(2)
		go func() { defer wg.Done(); c.Advance(time.Hour) }()
		go func() { defer wg.Done(); _ = c.Now(); _ = c.Offset() }()
	}
	wg.Wait()
	if got := c.Offset(); got != 8*time.Hour {
		t.Errorf("Offset = %v, veut 8h", got)
	}
}

// TestClock_interfacesSatisfaites fige le contrat : les deux
// implémentations doivent rester interchangeables à la compilation.
func TestClock_interfacesSatisfaites(t *testing.T) {
	t.Parallel()
	var _ Clock = System{}
	var _ Clock = NewControllable()
}
