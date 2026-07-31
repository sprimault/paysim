// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// Package payzen implemente le protocole PayZen / Lyra Collect (API REST V4).
//
// Reference : https://payzen.io/fr-FR/
// Version d'API visee : V4 (endpoints /api-payment/V4/*, retours kr-answer/kr-hash)
// Derniere verification contre la sandbox reelle : en attente du premier vecteur capture
//
// Le protocole V4 est distinct de l'API Formulaire V2 historique (champs vads_*)
// — voir la memoire projet reference-payzen-v4. Ce paquet ne connait ni V2 ni le
// SmartForm JavaScript cote client (SDK Krypton fourni par PayZen, servi depuis
// static.payzen.eu, utilise tel quel par l'application marchande).
package payzen

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// Sign calcule le kr-hash d'un kr-answer avec la cle HMAC-SHA-256 de la
// boutique. Retourne le hash encode en hexadecimal minuscule, format
// attendu par PayZen dans le champ POST kr-hash accompagnant kr-answer.
//
// C'est un HMAC-SHA-256 standard : la primitive est validee par les
// vecteurs RFC 4231. La conformite byte-pour-byte au format kr-answer
// realement emis par PayZen exige un vecteur de retour capture reel
// depuis la sandbox — voir invariant 4.
func Sign(krAnswer []byte, key string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(krAnswer)
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify recalcule le hash de krAnswer et le compare a hash (hex) en
// temps constant via hmac.Equal — indispensable pour eviter les timing
// attacks lors de la verification d'authenticite des retours.
//
// hash peut etre en majuscules ou en minuscules : hex.DecodeString
// accepte les deux. Un hash mal forme (longueur invalide, caracteres
// non-hex) retourne simplement false, jamais d'erreur — la fonction
// est un predicat de securite, pas un parser diagnostique.
func Verify(krAnswer []byte, hash, key string) bool {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(krAnswer)
	expected := mac.Sum(nil)

	received, err := hex.DecodeString(hash)
	if err != nil {
		return false
	}
	return hmac.Equal(expected, received)
}
