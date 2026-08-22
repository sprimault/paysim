// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"reflect"
	"testing"

	"github.com/sprimault/paysim/internal/providers/payzen"
)

// Parité des trois surfaces qui déclenchent une simulation.
//
// Le même webhook se déclenche par trois corps distincts :
// POST /paysim/simulate/browserReturn et /paysim/simulate/ipn côté
// fournisseur, POST /paysim/api/v1/payments/{uuid}/simulate côté API de
// contrôle. Chacun décrit son habillage — moyen annoncé, marque,
// portefeuille, verdict 3DS, refus, chaos — et chacun le déclare dans sa
// propre struct.
//
// Ils ont divergé sans bruit : la copie de l'API de contrôle avait perdu
// paymentMethodType et wallet, si bien qu'un parcours Apple Pay n'était
// jouable que par les routes du fournisseur, ni depuis l'interface ni
// depuis un scénario.
//
// Le partage d'un type embarqué serait la réponse habituelle. Il est
// exclu ici : tygo, qui produit les types TypeScript depuis ces structs,
// ne promeut pas les champs d'un embarquement anonyme et rendrait un
// objet imbriqué là où Go sérialise à plat. Le contrat annoncé au front
// cesserait de décrire le JSON réel — soit précisément le genre de
// mensonge que ce simulateur existe pour ne pas commettre.
//
// À défaut de pouvoir partager le type, on interdit la divergence.

// habillage liste les champs qui décrivent le webhook simulé, par nom de
// champ Go et tag JSON attendu. FormToken, ReturnURL, NotificationURL et
// Channel en sont volontairement absents : ils désignent la cible, pas
// l'habillage, et diffèrent légitimement d'une surface à l'autre.
var habillage = map[string]string{
	"Outcome":           "outcome",
	"PaymentMethodType": "paymentMethodType,omitempty",
	"CardBrand":         "cardBrand,omitempty",
	"ThreeDSStatus":     "threeDSStatus,omitempty",
	"ErrorCode":         "errorCode,omitempty",
	"ErrorMessage":      "errorMessage,omitempty",
	"Chaos":             "chaos,omitempty",
	"DeliveryDelayMs":   "deliveryDelayMs,omitempty",
}

// enAttenteDeVecteur porte les champs que les routes du fournisseur
// acceptent mais que l'API de contrôle n'expose délibérément pas, faute
// de capture réelle pour attester leur forme.
//
// Wallet : Paysim émet transactionDetails.wallet avec APPLE_PAY ou
// GOOGLEPAY, sur la foi d'une note de lecture et non d'un kr-answer
// capturé — testdata/raw est vide, et la documentation du fournisseur
// est rendue côté navigateur, donc non vérifiable automatiquement. Une
// source suggère même que le portefeuille apparaîtrait ailleurs, dans
// le type de carte. Tant que la question n'est pas tranchée par un
// vecteur, le champ n'est pas propagé à une surface de plus, et il
// n'est pas non plus retiré de celles qui l'ont déjà : le retirer
// changerait un comportement observable sur la même absence de preuve.
var enAttenteDeVecteur = map[string]string{
	"Wallet": "aucune capture réelle n'atteste la forme du portefeuille dans un kr-answer",
}

func TestSurfacesDeSimulationOntLeMemeHabillage(t *testing.T) {
	t.Parallel()

	surfaces := map[string]reflect.Type{
		"payzen.BrowserReturnRequest": reflect.TypeOf(payzen.BrowserReturnRequest{}),
		"payzen.IPNRequest":           reflect.TypeOf(payzen.IPNRequest{}),
		"api.SimulatePaymentRequest":  reflect.TypeOf(SimulatePaymentRequest{}),
	}

	// Types de référence, relevés sur la struct qui sert de modèle. Un
	// champ de même nom mais de type différent romprait la parité aussi
	// sûrement qu'un champ absent.
	modele := reflect.TypeOf(payzen.BrowserReturnRequest{})

	for nom, typ := range surfaces {
		for champ, tagAttendu := range habillage {
			f, ok := typ.FieldByName(champ)
			if !ok {
				t.Errorf("%s ne déclare pas %s : la surface ne peut pas jouer ce que les autres jouent", nom, champ)
				continue
			}
			if got := f.Tag.Get("json"); got != tagAttendu {
				t.Errorf("%s.%s tag json = %q, attendu %q — un même champ doit s'écrire pareil des deux côtés",
					nom, champ, got, tagAttendu)
			}
			ref, _ := modele.FieldByName(champ)
			if f.Type != ref.Type {
				t.Errorf("%s.%s type = %s, attendu %s", nom, champ, f.Type, ref.Type)
			}
		}
	}
}

// TestHabillageCouvreToutLeModele garde la liste ci-dessus honnête : un
// champ d'habillage ajouté à BrowserReturnRequest sans être ajouté à
// `habillage` échapperait au test précédent, qui ne vérifie que ce qu'on
// lui donne. Les champs de ciblage sont les seuls tolérés hors liste.
func TestHabillageCouvreToutLeModele(t *testing.T) {
	t.Parallel()

	ciblage := map[string]bool{
		"FormToken": true,
		"ReturnURL": true,
	}

	modele := reflect.TypeOf(payzen.BrowserReturnRequest{})
	for i := range modele.NumField() {
		nom := modele.Field(i).Name
		if ciblage[nom] {
			continue
		}
		if _, attente := enAttenteDeVecteur[nom]; attente {
			continue
		}
		if _, suivi := habillage[nom]; !suivi {
			t.Errorf("BrowserReturnRequest.%s n'est ni du ciblage ni suivi par `habillage` : "+
				"l'ajouter à la liste, ou à enAttenteDeVecteur si aucune capture ne l'atteste", nom)
		}
	}
}
