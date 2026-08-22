// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package store

import "time"

// EventRecord est la représentation persistante d'un événement du bus
// applicatif. Utilisé par SSE pour permettre un catch-up client
// après un redémarrage du serveur : le navigateur envoie
// Last-Event-ID, le bus lit depuis le ring buffer mémoire puis
// complète depuis EventRepository si nécessaire.
//
// Data est stocké en JSON opaque — le bus ne connaît pas la
// structure des payloads (map, struct provider…). À la relecture,
// Data est désérialisé en `map[string]any` pour rester générique.
type EventRecord struct {
	// ID est le compteur monotone du bus. C'est lui qui permet à un
	// client SSE reconnecté de reprendre où il s'était arrêté après un
	// redémarrage, sans trou ni doublon.
	ID uint64

	// Type nomme l'événement.
	Type string

	// At est l'instant de publication, en UTC.
	At time.Time

	// DataJSON est la charge utile sérialisée. Le store ne l'interprète
	// pas : il transporte ce que le bus lui donne.
	DataJSON string
}

// EventRepository est le contrat de persistance des événements bus.
type EventRepository interface {
	// Save persiste un event. ID assigné en amont par le bus (compteur
	// atomique), pas généré côté repository — cohérent avec le
	// contrat SSE Last-Event-ID où chaque event a un ID unique
	// monotone.
	Save(rec EventRecord) error

	// Since retourne les événements avec un ID strictement supérieur
	// à lastID, dans l'ordre croissant, au plus limit d'entre eux.
	//
	// La borne n'est pas un confort : la table n'est purgée par
	// personne, et un client SSE qui se reconnecte avec un
	// Last-Event-ID ancien — ou absent, donc zéro — demanderait sinon
	// la totalité de l'historique d'un coup, en mémoire. Un limit
	// négatif ou nul est traité comme « aucune ligne » : c'est un
	// appelant fautif, pas une invitation à tout charger.
	Since(lastID uint64, limit int) ([]EventRecord, error)

	// DeleteBefore supprime les événements plus anciens que id inclus.
	// Utile pour éviter la croissance illimitée en prod ; non appelé
	// automatiquement en v1.
	DeleteBefore(id uint64) (int, error)

	// Close libère les ressources sous-jacentes.
	Close() error
}
