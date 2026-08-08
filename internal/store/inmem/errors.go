// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package inmem

import (
	"errors"

	"github.com/sprimault/paysim/internal/store"
)

// errNilRecord aligne le refus d'un Save(nil) sur celui des dépôts
// SQLite : c'est un défaut d'appel, pas un cas de persistance.
var errNilRecord = errors.New("Save(nil)")

// Conformité vérifiée à la compilation : une méthode qui diverge de
// l'interface se verrait sinon au câblage dans main.go, c'est-à-dire à
// l'exécution, et pour le mode qui n'est justement testé nulle part.
var (
	_ store.PaymentRepository       = (*PaymentsRepository)(nil)
	_ store.PaymentMethodRepository = (*PaymentMethodsRepository)(nil)
	_ store.SubscriptionRepository  = (*SubscriptionsRepository)(nil)
)
