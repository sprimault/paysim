// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// Package delivery livre les webhooks sortants. C'est le point unique de
// sortie du binaire vers les URL de notification des marchands : aucun
// paquet fournisseur ne fait de POST HTTP direct — chaque adaptateur
// construit un Webhook (avec sa signature et ses headers déjà appliqués)
// et le remet ici. C'est l'invariant 2 qui rend le moteur de chaos
// uniforme : la seule façon d'injecter un délai, un doublon ou un
// désordre, c'est de le faire à un endroit — cet endroit.
package delivery

import "time"

// Webhook est une requête HTTP sortante prête à partir. La signature du
// provider, les headers et le body sont déjà construits par l'adaptateur
// qui a rempli ce Webhook. Ce paquet ne fait que du transport pur — il
// ne connaît ni PayZen ni Stripe.
//
// Une fois Enqueue appelé, l'appelant ne doit plus modifier Body ni
// Headers : la valeur est mise en file et lue par le worker sans copie.
type Webhook struct {
	// ID est un identifiant de traçage. Sert au log et au rejeu manuel
	// prévu à partir de la phase 3.
	ID string

	// URL cible : typiquement la CallbackURL du marchand, ou l'URL de
	// notification configurée dans le back-office PSP.
	URL string

	// Headers HTTP à poser sur la requête. Le Content-Type et la
	// signature (par exemple kr-hash pour PayZen V4) doivent y figurer.
	Headers map[string]string

	// Body est le corps de la requête, déjà sérialisé (JSON ou form
	// URL-encoded selon le protocole du provider).
	Body []byte

	// Outcome est le résultat métier que ce webhook annonce, dans le
	// vocabulaire du provider ("PAID", "UNPAID"… pour PayZen). Déclaré
	// par l'adaptateur au moment d'émettre, jamais déduit du Body :
	// delivery ne sait pas lire un kr-answer, et n'a pas à l'apprendre
	// pour chaque provider ajouté. Vide quand le webhook n'annonce pas
	// de résultat de paiement.
	//
	// Sert à distinguer, côté assertions, « le webhook a bien été
	// remis » (Status) de « le webhook annonçait un paiement accepté »
	// (Outcome) — deux questions que le champ Status seul confondait.
	Outcome string

	// Attempts compte les tentatives. Incrémenté par le worker avant
	// chaque envoi. En phase 1 le worker ne tente qu'une fois ; la
	// logique de retry arrive avec le chaos en phase 2.
	Attempts int

	// CreatedAt est l'instant d'entrée dans la file, UTC. Si laissé à
	// zéro par l'appelant, Enqueue le fixe à time.Now().UTC().
	CreatedAt time.Time

	// LastTryAt est l'instant de la dernière tentative, UTC. Vaut la
	// valeur zéro tant que le worker n'a pas traité le webhook.
	LastTryAt time.Time

	// Delay est le délai avant premier envoi. Zéro = immédiat.
	// Permet aux appelants du chaos de simuler un webhook retardé
	// (out-of-order avec un second webhook non retardé) sans bloquer
	// les autres livraisons — le scheduler lance chaque delivery en
	// goroutine indépendante depuis le vertical 3 phase 2.
	Delay time.Duration
}
