// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

import (
	"bytes"
	"strings"
	"testing"
)

// rfc4231Cases contient les vecteurs de reference IETF pour HMAC-SHA-256.
// Ils sont publics, calcules independamment de notre code, et valident la
// primitive HMAC en tant que telle — pas la compatibilite au format
// kr-answer emis par PayZen, qui exige un vecteur reel capture depuis la
// sandbox (invariant 4, voir la memoire reference-payzen-sandbox-topdata
// pour la marche a suivre).
//
// Cas selectionnes : 1 (cle courte binaire), 2 (cle texte), 6 (cle plus
// longue qu'un bloc SHA-256), 7 (cle et donnees plus longues qu'un bloc).
// Le cas 5 (troncature) et 3/4 (donnees et cles moyennes) sont couverts
// par les autres.
var rfc4231Cases = []struct {
	name string
	key  []byte
	data []byte
	hash string
}{
	{
		name: "case1 short binary key",
		key:  bytes.Repeat([]byte{0x0b}, 20),
		data: []byte("Hi There"),
		hash: "b0344c61d8db38535ca8afceaf0bf12b881dc200c9833da726e9376c2e32cff7",
	},
	{
		name: "case2 short text key",
		key:  []byte("Jefe"),
		data: []byte("what do ya want for nothing?"),
		hash: "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843",
	},
	{
		name: "case6 large key",
		key:  bytes.Repeat([]byte{0xaa}, 131),
		data: []byte("Test Using Larger Than Block-Size Key - Hash Key First"),
		hash: "60e431591ee0b67f0d8a26aacbf5b77f8e0bc6213728c5140546040f0ee37f54",
	},
	{
		name: "case7 large key and data",
		key:  bytes.Repeat([]byte{0xaa}, 131),
		data: []byte("This is a test using a larger than block-size key and a larger than block-size data. The key needs to be hashed before being used by the HMAC algorithm."),
		hash: "9b09ffa71b942fcb27635fbcd5b0e944bfdc63644f0713938a7f51535c3a35e2",
	},
}

func TestSignMatchesRFC4231(t *testing.T) {
	t.Parallel()
	for _, c := range rfc4231Cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := Sign(c.data, string(c.key))
			if got != c.hash {
				t.Errorf("Sign() = %s, veut %s", got, c.hash)
			}
		})
	}
}

func TestVerifyAcceptsCorrectHash(t *testing.T) {
	t.Parallel()
	for _, c := range rfc4231Cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if !Verify(c.data, c.hash, string(c.key)) {
				t.Errorf("Verify() sur hash correct = false")
			}
		})
	}
}

func TestVerifyIsCaseInsensitive(t *testing.T) {
	t.Parallel()
	// hex.DecodeString accepte les deux casses. Un vrai kr-hash de PayZen
	// est en minuscules, mais un middleware peut le normaliser en cours de
	// route (nginx, proxy). On tolere les deux plutot que de casser sur
	// une variation invisible.
	for _, c := range rfc4231Cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if !Verify(c.data, strings.ToUpper(c.hash), string(c.key)) {
				t.Errorf("Verify() sur hash en majuscules = false, veut insensibilite a la casse")
			}
		})
	}
}

func TestVerifyRejects(t *testing.T) {
	t.Parallel()
	// Data et cle plausibles pour un contexte kr-answer PayZen (JSON,
	// cle base64-like). Les valeurs elles-memes n'ont pas d'importance
	// — l'objectif est de verifier que Verify refuse toute alteration.
	const key = "clef-de-test-arbitraire"
	data := []byte(`{"orderStatus":"PAID","transactions":[{"uuid":"abc"}]}`)
	correct := Sign(data, key)

	cases := []struct {
		name string
		hash string
		data []byte
		key  string
	}{
		{"hash altere un caractere", flipFirstHexChar(correct), data, key},
		{"cle differente", correct, data, "autre-clef"},
		{"body altere", correct, []byte(`{"orderStatus":"REFUSED"}`), key},
		{"hash tronque", correct[:len(correct)-2], data, key},
		{"hash non hex", "zzzzzzzz", data, key},
		{"hash vide", "", data, key},
		{"hash trop long", correct + "00", data, key},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if Verify(c.data, c.hash, c.key) {
				t.Errorf("Verify(%s) = true, veut false", c.name)
			}
		})
	}
}

// lyraOfficialCases contient les vecteurs de test hardcodés dans le
// SDK Java officiel Lyra (lyra/rest-api-server-java-sdk), fichier
// src/test/java/com/lyra/rest/client/ClientCryptUtilTest.java.
//
// Ce sont des calculs cryptographiques (message + clé → hash), pas de
// la propriété intellectuelle : on peut les recopier au titre de
// l'interopérabilité. Ils constituent la SEULE référence officielle
// Lyra disponible publiquement qui permette de valider notre
// implémentation byte-pour-byte contre le SDK officiel.
//
// Confirmation cruciale portée par ces vecteurs : Lyra fait
// HMAC-SHA-256(message, key) SANS concaténer la clé au message —
// contrairement à ce que font certaines libs PHP OSS historiques.
// Notre implémentation Go Sign() respecte cette convention.
var lyraOfficialCases = []struct {
	name string
	data []byte
	key  string
	hash string
}{
	{
		name: "empty answer",
		data: []byte(""),
		key:  "ktM7bSeTJpclvpm4eEE9N0LIyoxUvsQ9AAYbQI1xQx7Qh",
		hash: "a95c2b13d50d57858ff38e7abd76c39d644fd5d1cfdcc360e4c61f2fc48d4a5e",
	},
}

func TestSignMatchesLyraOfficial(t *testing.T) {
	t.Parallel()
	for _, c := range lyraOfficialCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := Sign(c.data, c.key)
			if got != c.hash {
				t.Errorf("Sign() = %s\n     veut %s", got, c.hash)
			}
		})
	}
}

func TestVerifyAcceptsLyraOfficial(t *testing.T) {
	t.Parallel()
	for _, c := range lyraOfficialCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if !Verify(c.data, c.hash, c.key) {
				t.Errorf("Verify() sur vecteur Lyra officiel = false")
			}
		})
	}
}

func TestSignVerifyRoundTrip(t *testing.T) {
	t.Parallel()
	// Sanity check : ce que Sign produit doit passer Verify. Redondant
	// avec les cas RFC 4231, mais garantit qu'on n'a pas divergence
	// entre les deux fonctions si un jour l'une d'elles evolue.
	const key = "clef-round-trip"
	payloads := [][]byte{
		[]byte(`{}`),
		[]byte(`{"orderStatus":"PAID"}`),
		[]byte(strings.Repeat("A", 4096)),
		{}, // corps vide
		{0x00, 0x01, 0x02, 0xff}, // octets binaires
	}
	for i, p := range payloads {
		hash := Sign(p, key)
		if !Verify(p, hash, key) {
			t.Errorf("payload #%d : aller-retour echoue", i)
		}
	}
}

// flipFirstHexChar remplace le premier chiffre hex par un autre valide,
// pour construire un hash de meme longueur mais avec un bit different.
func flipFirstHexChar(s string) string {
	if len(s) == 0 {
		return s
	}
	b := []byte(s)
	if b[0] == '0' {
		b[0] = '1'
	} else {
		b[0] = '0'
	}
	return string(b)
}
