// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// Package payzen implemente le protocole PayZen / Lyra Collect (API REST V4).
//
// Reference : https://payzen.io/fr-FR/
// Version d'API visee : V4 (endpoints /api-payment/V4/*, retours kr-answer/kr-hash)
// Derniere verification contre le SDK officiel Lyra : 2026-07-31
// (vecteur EMPTY_ANSWER extrait de rest-api-server-java-sdk /
// ClientCryptUtilTest.java, validation byte-pour-byte de la primitive
// et du format de sortie)
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
// C'est un HMAC-SHA-256 standard, verifie de deux facons independantes :
// (1) primitive validee par les vecteurs RFC 4231 ; (2) sortie
// verifiee byte-pour-byte contre le SDK Java officiel Lyra
// (rest-api-server-java-sdk / ClientCryptUtilTest.java). Point cle :
// la cle n'est PAS concatenee au message avant hachage, contrairement
// a ce que font certaines libs PHP OSS historiques — le SDK officiel
// fait juste HMAC(message, key).
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
