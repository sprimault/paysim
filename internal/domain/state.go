// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package domain

// State est l'état courant d'un paiement dans sa machine à états.
// Le graphe complet — transitions valides et terminaux — est documenté dans
// docs/states.md.
type State string

// Ensemble fermé des états possibles. La valeur textuelle est la
// représentation canonique : elle sera utilisée telle quelle en JSON, dans les
// logs, et dans les protocoles exposés en phase 1+. Les adaptateurs
// fournisseur (internal/providers, phase 1) mappent leurs codes propres vers
// ces valeurs — jamais l'inverse : le domaine ne connaît pas les codes
// spécifiques d'un PSP.
const (
	StateInitiated         State = "initiated"
	StateAuthorized        State = "authorized"
	StateCaptured          State = "captured"
	StateRefunded          State = "refunded"
	StatePartiallyRefunded State = "partially_refunded"
	StateDeclined          State = "declined"
	StateExpired           State = "expired"
	StateChargeback        State = "chargeback"
)

// IsTerminal indique qu'aucune transition n'est plus possible depuis cet état.
// À noter : chargeback est terminal alors qu'il est atteignable depuis des
// états eux-mêmes non-terminaux (captured, refunded, partially_refunded).
// C'est la seule branche où le graphe « revient en arrière » fonctionnellement
// — la rétrofacturation clôt définitivement le paiement.
func (s State) IsTerminal() bool {
	switch s {
	case StateRefunded, StateDeclined, StateExpired, StateChargeback:
		return true
	}
	return false
}
