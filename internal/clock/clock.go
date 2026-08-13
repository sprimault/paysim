// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// Package clock fournit la source de temps du simulateur.
//
// Tout horodatage qu'un marchand peut observer — journal d'événements,
// serverDate d'un kr-answer, dates de livraison — passe par ici, ce qui
// permet de le faire avancer à la demande plutôt que d'attendre. Ce
// qui relève de la mesure ou de l'instrumentation garde le temps réel
// et n'utilise pas ce paquet : une durée de requête qui suivrait une
// horloge simulée deviendrait absurde à la première avance.
//
// Feuille du graphe de dépendances, comme internal/format : n'importe
// rien d'autre dans internal/, et doit le rester — le domaine en
// dépend, et le domaine ne dépend de presque rien.
package clock

import (
	"sync"
	"time"
)

// Clock est la source de temps. Volontairement réduite à Now : les
// attentes et les temporisations restent gérées par time.After et
// time.Sleep, qui mesurent une durée et non un instant.
type Clock interface {
	Now() time.Time
}

// System est l'horloge de production. Le zéro-valeur suffit, elle
// s'injecte donc sans construction.
type System struct{}

// Now retourne l'heure courante en UTC. Tout le projet travaille en UTC
// en interne, la conversion n'ayant lieu qu'à l'affichage.
func (System) Now() time.Time { return time.Now().UTC() }

// Controllable avance le temps à la demande, en ajoutant un décalage
// cumulé à l'heure réelle.
//
// Un décalage plutôt qu'un instant figé, pour deux raisons. Le dépôt
// mémoire trie les paiements par date de modification et départage par
// UUID parce que plusieurs écritures tiennent dans la même nanoseconde ;
// une horloge gelée ferait de tout l'ordre d'affichage un tri par UUID.
// Et une durée mesurée au cours d'une requête ressortirait à zéro.
//
// Le décalage n'est jamais persisté : une instance qui redémarre repart
// à l'heure réelle. Se réveiller silencieusement quatre jours en avant
// serait exactement le genre de mensonge que ce simulateur existe pour
// éviter.
type Controllable struct {
	mu     sync.RWMutex
	offset time.Duration
}

// NewControllable construit une horloge alignée sur l'heure réelle.
// Décalage nul : la capacité existe mais ne fait rien tant qu'on ne
// l'actionne pas, comme l'exige la règle du chaos jamais actif par
// défaut.
func NewControllable() *Controllable { return &Controllable{} }

// Now retourne l'heure réelle augmentée du décalage courant.
func (c *Controllable) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Now().UTC().Add(c.offset)
}

// Advance ajoute une durée au décalage et retourne l'heure obtenue.
//
// Les avances se cumulent : deux appels d'un jour placent l'instance
// deux jours plus loin. Une durée négative est refusée par l'appelant
// (elle produirait un updatedAt antérieur au createdAt) ; ici elle
// serait simplement soustraite, la garde vit dans l'API.
func (c *Controllable) Advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offset += d
	return time.Now().UTC().Add(c.offset)
}

// Offset retourne le décalage cumulé. Zéro signifie que l'instance est
// à l'heure réelle.
func (c *Controllable) Offset() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.offset
}

// Reset ramène l'instance à l'heure réelle.
func (c *Controllable) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offset = 0
}
