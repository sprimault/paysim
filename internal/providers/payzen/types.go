// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package payzen

import (
	"encoding/json"
	"time"

	"github.com/sprimault/paysim/internal/domain"
	"github.com/sprimault/paysim/internal/format"
)

// APIResponse est l'enveloppe standard des reponses V4 : status + answer
// opaque. PayZen renvoie beaucoup plus de champs (webService, version,
// serverDate, mode, applicationVersion, ticket...), on les ajoute au fur
// et a mesure si un client s'en plaint — pas de sur-modelisation a froid.
type APIResponse struct {
	// Status vaut SUCCESS ou ERROR. C'est lui qu'un client PayZen
	// inspecte avant de décoder Answer — un HTTP 200 ne suffit pas.
	Status string `json:"status"`

	// Answer porte la réponse propre à l'endpoint, ou un APIError quand
	// Status vaut ERROR. Laissé brut pour que l'appelant décide du type
	// à décoder selon l'appel qu'il a fait.
	Answer json.RawMessage `json:"answer"`
}

// APIError est la structure retournee dans answer quand status vaut
// "ERROR". Format aligne sur celui de PayZen pour rester interoperable
// avec les SDK marchand qui inspectent ces champs.
type APIError struct {
	// ErrorCode est le code machine, ErrorMessage sa version lisible.
	// Chez Paysim les codes portent le préfixe PAYSIM_ pour ne pas se
	// faire passer pour des codes PayZen réels.
	ErrorCode    string `json:"errorCode"`
	ErrorMessage string `json:"errorMessage"`

	// DetailedErrorCode et DetailedErrorMessage précisent la cause
	// quand elle est connue. PayZen s'en sert pour distinguer un refus
	// bancaire d'une erreur d'intégration ; Paysim les laisse souvent
	// vides plutôt que d'inventer une précision qu'il n'a pas.
	DetailedErrorCode    string `json:"detailedErrorCode,omitempty"`
	DetailedErrorMessage string `json:"detailedErrorMessage,omitempty"`
}

// Codes d'erreur Paysim. Prefixe PAYSIM_ pour ne pas se confondre avec
// les codes reels de PayZen (INT_010, PSP_010, ACQ_010...). Le jour ou
// un client attend un code PayZen precis, on mappera ici.
const (
	ErrCodeInvalidRequest  = "PAYSIM_INVALID_REQUEST"
	ErrCodeInvalidAmount   = "PAYSIM_INVALID_AMOUNT"
	ErrCodeInvalidCurrency = "PAYSIM_INVALID_CURRENCY"
	ErrCodeInvalidPayment  = "PAYSIM_INVALID_PAYMENT"
	ErrCodeUUIDUnknown     = "PAYSIM_UUID_UNKNOWN"
	// #nosec G101 -- codes d'erreur, pas des secrets.
	ErrCodeTokenUnknown         = "PAYSIM_TOKEN_UNKNOWN"
	ErrCodeSubscriptionUnknown  = "PAYSIM_SUBSCRIPTION_UNKNOWN"
	ErrCodeStoreFailure         = "PAYSIM_STORE_FAILURE"
	ErrCodePaymentMethodUnknown = "PAYSIM_PAYMENT_METHOD_UNKNOWN"
	ErrCodeExpiredCard          = "PAYSIM_EXPIRED_CARD"
	ErrCodeRevokedCard          = "PAYSIM_REVOKED_CARD"
	// #nosec G101 -- code d'erreur, pas un secret.
	ErrCodeInvalidCard = "PAYSIM_INVALID_CARD"

	// ErrCodeUnauthorized accompagne un 401. Préfixé PAYSIM_ comme les
	// autres : inventer un code d'erreur PayZen qui n'existe pas
	// tromperait un intégrateur qui le chercherait dans leur
	// documentation.
	ErrCodeUnauthorized = "PAYSIM_UNAUTHORIZED"

	// ErrCodeRefused habille un paiement refusé au niveau PSP. Le motif
	// bancaire, lui, vit dans detailedErrorCode : c'est un code ISO 8583
	// non préfixé, parce qu'il vient de l'acquéreur et non de nous.
	ErrCodeRefused = "PAYSIM_REFUSED"
)

// CreatePaymentRequest est le corps JSON attendu par POST
// /api-payment/V4/Charge/CreatePayment. Les noms de champs sont ceux
// utilises par PayZen — on les recopie tels quels (regle providers.md).
//
// ReturnURL et NotificationURL sont des extensions propres a Paysim
// (non-standard PayZen) : le vrai back-office porte cette configuration
// statiquement, mais un simulateur profite d'un contrat par requete.
// Optionnelles — si absentes, l'appel de simulation devra les fournir.
type CreatePaymentRequest struct {
	// OrderID est la référence de commande du marchand, libre.
	OrderID string `json:"orderId"`

	// Amount en centimes entiers, Currency en ISO 4217. Zéro est
	// accepté : c'est l'enrôlement pur, qui enregistre une carte sans
	// rien débiter.
	Amount   format.Amount `json:"amount"`
	Currency string        `json:"currency"`

	// FormAction déclare l'intention (PAYMENT, REGISTER,
	// REGISTER_PAY, ASK_REGISTER_PAY). Conservée et restituée, mais
	// sans effet sur l'enrôlement côté simulateur.
	FormAction string `json:"formAction,omitempty"`

	// Customer et Metadata sont le contexte marchand, restitués tels
	// quels dans le webhook et jamais interprétés.
	Customer Customer          `json:"customer,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`

	// ReturnURL et NotificationURL ciblent le retour navigateur et
	// l'IPN. Extensions Paysim : le vrai PayZen les tient de la
	// configuration de boutique. Absentes, les endpoints de simulation
	// retombent sur PAYSIM_CALLBACK_URL.
	ReturnURL       string `json:"returnUrl,omitempty"`
	NotificationURL string `json:"notificationUrl,omitempty"`

	// PaymentMethodToken déclenche un rejeu one-click : plutôt que de
	// démarrer un formulaire, Paysim créera un paiement directement à
	// partir du moyen de paiement stocké. Vérifie l'expiration et la
	// révocation avant capture. Ignoré si Card est également fourni.
	PaymentMethodToken string `json:"paymentMethodToken,omitempty"`

	// Card est une extension Paysim (hors périmètre PayZen réel où le
	// PAN transite par le SmartForm client, pas par l'API marchand).
	// Fourni pour permettre à un scénario ou à un test d'intégration
	// d'enregistrer un moyen de paiement directement, sans SmartForm.
	// Combiné avec formAction=REGISTER_PAY, produit un paymentMethodToken.
	Card *Card `json:"card,omitempty"`
}

// CreatePaymentAnswer est le contenu de answer sur succes.
type CreatePaymentAnswer struct {
	// FormToken est opaque côté marchand : il le passe au SmartForm, ou
	// aux endpoints de simulation chez Paysim. Sans rapport avec le
	// paymentMethodToken, qui désigne une carte enregistrée.
	FormToken string `json:"formToken"`
}

// TransactionGetRequest est le corps de POST /api-payment/V4/Transaction/Get.
type TransactionGetRequest struct {
	// UUID de la transaction à relire.
	UUID string `json:"uuid"`
}

// TransactionGetAnswer est le resume d'une transaction retourne au marchand.
// Miroir simplifie de la structure des transactions[0] dans kr-answer :
// on garde ce qui est utile pour le controle cote marchand, on n'invente
// pas ce qu'on ne peut pas remplir.
type TransactionGetAnswer struct {
	// UUID et OrderID identifient la transaction et la commande.
	UUID    string `json:"uuid"`
	OrderID string `json:"orderId"`

	// Amount en centimes entiers, Currency en ISO 4217.
	Amount   format.Amount `json:"amount"`
	Currency string        `json:"currency"`

	// OrderStatus porte ici l'état du domaine, pas le vocabulaire
	// PayZen du kr-answer. Divergence assumée : cet endpoint sert au
	// contrôle côté marchand, où l'état canonique est plus utile qu'un
	// PAID qui recouvre autorisation et capture.
	OrderStatus domain.State `json:"orderStatus"`

	// PaymentMethodType est le moyen employé, quand il est connu.
	PaymentMethodType string `json:"paymentMethodType,omitempty"`

	// CreationDate et LastUpdateDate en ISO 8601 UTC.
	CreationDate   string `json:"creationDate"`
	LastUpdateDate string `json:"lastUpdateDate"`

	// Customer et Metadata restituent le contexte marchand.
	Customer Customer          `json:"customer,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Card est l'input pour enregistrer un moyen de paiement, tel que
// fourni par les scénarios YAML ou les tests d'intégration. Reprend
// la structure minimale que le SmartForm envoie au PSP en usage réel.
// Le PAN entre en clair — aucun rejet, aucune validation Luhn,
// aveugle sur le contenu.
type Card struct {
	// PAN est le numéro complet, en clair. Aucune validation Luhn : le
	// simulateur est aveugle au contenu, hormis les quatre PAN de test
	// réservés qui déclenchent un refus.
	PAN string `json:"pan"`

	// ExpiryMonth (1-12) et ExpiryYear (4 chiffres). Une date passée
	// fait refuser tout débit, ce qui en fait un levier de test.
	ExpiryMonth int `json:"expiryMonth"`
	ExpiryYear  int `json:"expiryYear"`

	// Brand est la marque. Optionnelle : déduite du BIN si absente.
	Brand string `json:"brand,omitempty"`

	// HolderName est le nom du porteur ("DUPONT JEAN"). Optionnel :
	// un wallet n'en transmet pas. Conservé tel quel, sans
	// normalisation de casse — le marchand le relit à l'identique.
	HolderName string `json:"holderName,omitempty"`

	// Country, ProductCategory et IssuerName décrivent la carte telle
	// que l'émetteur la caractérise. Optionnels, mais ce sont eux qui
	// rendent testables la carte étrangère, la carte de débit et le
	// routage par banque — figés, ils interdisaient ces scénarios.
	// Défauts appliqués au rendu : FR, CREDIT, PAYSIM.
	Country         string `json:"country,omitempty"`         // ISO 3166-1 alpha-2
	ProductCategory string `json:"productCategory,omitempty"` // CREDIT, DEBIT, PREPAID
	IssuerName      string `json:"issuerName,omitempty"`
}

// Customer et BillingDetails miroir de la structure PayZen — aucun
// champ obligatoire cote domain, on stocke pour le rendre dans les
// retours. Les noms conservent la casse PayZen.
type Customer struct {
	// Email de l'acheteur. PayZen s'en sert pour ses notifications ;
	// Paysim le conserve et le restitue sans l'employer.
	Email string `json:"email,omitempty"`

	// Reference est l'identifiant du client côté marchand. C'est elle
	// qui permet de rapprocher un paiement d'un compte sans dépendre de
	// la metadata — un intégrateur qui s'y fie doit la retrouver dans
	// le kr-answer, sans quoi elle disparaît sans erreur au décodage.
	Reference string `json:"reference,omitempty"`

	// BillingDetails porte l'adresse de facturation. Toujours sérialisé,
	// même vide : c'est une struct, et omitempty n'a aucun effet
	// dessus en Go.
	BillingDetails BillingDetails `json:"billingDetails,omitempty"`

	// ShippingDetails porte l'adresse de livraison, ExtraDetails le
	// contexte navigateur exploité par l'antifraude. Comme
	// BillingDetails, ce sont des structs : toujours sérialisées, même
	// vides.
	//
	// Ces deux blocs existent chez PayZen depuis toujours. Paysim ne
	// les modélisait pas, donc un marchand qui les envoyait les voyait
	// disparaître au décodage, sans erreur ni trace — le même défaut
	// silencieux que customer.reference avant la v0.4.10.
	ShippingDetails ShippingDetails `json:"shippingDetails,omitempty"`
	ExtraDetails    ExtraDetails    `json:"extraDetails,omitempty"`
}

// BillingDetails represente l'adresse de facturation. Aucun champ
// n'est obligatoire — le simulateur les propage tels quels.
type BillingDetails struct {
	// Language est la langue de facturation (code ISO 639-1), Title la
	// civilité. PayZen s'en sert pour ses courriels ; Paysim les
	// conserve et les restitue sans les employer.
	Language string `json:"language,omitempty"`
	Title    string `json:"title,omitempty"`

	// Identité du payeur.
	FirstName string `json:"firstName,omitempty"`
	LastName  string `json:"lastName,omitempty"`

	// Adresse postale. Country est en ISO 3166-1 alpha-2 — à ne pas
	// confondre avec le pays de la carte, dans KrCardDetails : l'un
	// est celui du client, l'autre celui de l'émetteur.
	Address string `json:"address,omitempty"`
	City    string `json:"city,omitempty"`
	ZipCode string `json:"zipCode,omitempty"`
	Country string `json:"country,omitempty"`
}

// ShippingDetails represente l'adresse de livraison. Aucun champ n'est
// obligatoire — le simulateur les propage tels quels.
//
// Category, ShippingSpeed et ShippingMethod sont des énumérations chez
// PayZen. Elles restent des chaînes libres ici, sans validation : un
// simulateur qui refuserait une valeur que le vrai accepte serait un
// piège, et Paysim ne les interprète jamais. Même arbitrage que le PAN,
// accepté sans contrôle de Luhn.
type ShippingDetails struct {
	// Category distingue un particulier d'une entreprise : PRIVATE ou
	// COMPANY. LegalName et IdentityCode ne valent que pour la seconde.
	Category  string `json:"category,omitempty"`
	LegalName string `json:"legalName,omitempty"`

	// IdentityCode est le numéro d'identification légale du
	// destinataire (SIRET et équivalents).
	IdentityCode string `json:"identityCode,omitempty"`

	// Identité et contact du destinataire. Distincts de ceux de
	// BillingDetails : on livre couramment à quelqu'un d'autre que le
	// payeur.
	FirstName   string `json:"firstName,omitempty"`
	LastName    string `json:"lastName,omitempty"`
	PhoneNumber string `json:"phoneNumber,omitempty"`

	// Adresse postale. PayZen la découpe plus finement que l'adresse de
	// facturation — numéro, complément et arrondissement séparés — parce
	// que les règles antifraude comparent ces éléments un à un.
	// Country en ISO 3166-1 alpha-2.
	StreetNumber string `json:"streetNumber,omitempty"`
	Address      string `json:"address,omitempty"`
	Address2     string `json:"address2,omitempty"`
	District     string `json:"district,omitempty"`
	ZipCode      string `json:"zipCode,omitempty"`
	City         string `json:"city,omitempty"`
	State        string `json:"state,omitempty"`
	Country      string `json:"country,omitempty"`

	// DeliveryCompanyName nomme le transporteur.
	DeliveryCompanyName string `json:"deliveryCompanyName,omitempty"`

	// ShippingSpeed vaut STANDARD, EXPRESS ou PRIORITY.
	ShippingSpeed string `json:"shippingSpeed,omitempty"`

	// ShippingMethod décrit le mode de remise : RELAY_POINT,
	// PACKAGE_DELIVERY_COMPANY, DIGITAL_GOOD, ETICKET, PICKUP_POINT,
	// RECLAIM_IN_SHOP… La liste PayZen en compte une quinzaine, et
	// elle bouge — raison de plus pour ne pas la figer ici.
	ShippingMethod string `json:"shippingMethod,omitempty"`
}

// ExtraDetails porte le contexte navigateur de l'acheteur. PayZen le
// transmet à ses règles antifraude et à l'authentification 3DS ; Paysim
// le conserve et le restitue sans l'employer.
//
// C'est le bloc à renseigner pour rejouer un scénario de refus pour
// risque : sans lui, rien ne distingue deux tentatives.
type ExtraDetails struct {
	// IPAddress est l'adresse de l'acheteur, FingerPrintID l'empreinte
	// calculée par le script de collecte de PayZen.
	//
	// Le nom du champ JSON est bien "ipAddress" — vocabulaire du
	// protocole, recopié tel quel.
	IPAddress     string `json:"ipAddress,omitempty"`
	FingerPrintID string `json:"fingerPrintId,omitempty"`

	// BrowserUserAgent et BrowserAccept reprennent les en-têtes HTTP du
	// navigateur, que 3DS2 exige dans la demande d'authentification.
	BrowserUserAgent string `json:"browserUserAgent,omitempty"`
	BrowserAccept    string `json:"browserAccept,omitempty"`
}

// Transaction est le contexte complet d'un paiement PayZen simule.
// Un formToken opaque cote marchand pointe sur cette structure. Elle
// porte aussi l'aggregat domain.Payment, qui reste la source de verite
// pour la machine a etats — les autres champs sont metadonnees pour
// remplir les payloads V4.
//
// ReturnURL et NotificationURL sont stockees ici a titre de valeurs
// par defaut pour les appels de simulation. L'appel de simulation
// peut les surcharger.
type Transaction struct {
	// FormToken est l'identifiant opaque rendu au marchand par
	// CreatePayment. C'est la clé de reprise : les endpoints de
	// simulation retrouvent la transaction par ce jeton, pas par UUID.
	FormToken string

	// UUID identifie la transaction, et se retrouve tel quel dans le
	// kr-answer et l'API de contrôle.
	UUID string

	// Brand est la marque Lyra qui porte ce paiement — payzen,
	// systempay, sogecommerce, scellius ou lyra. Vide vaut la marque par
	// défaut de l'adaptateur.
	//
	// Portée par le paiement et non par l'instance : une instance peut
	// héberger plusieurs intégrations à la fois, chacune étiquetée. Le
	// trafic arrivant par les routes du protocole prend la marque de
	// l'instance, ces routes n'en transportant aucune.
	Brand string

	// OrderID est la référence de commande du marchand, libre.
	OrderID string

	// Amount en centimes entiers, Currency en ISO 4217. Zéro est
	// légitime — c'est l'enrôlement pur, qui n'appelle aucun débit.
	Amount   format.Amount
	Currency string

	// FormAction porte l'intention déclarée par le marchand
	// (PAYMENT, REGISTER, REGISTER_PAY…). Conservé pour restitution.
	FormAction string

	// Card est la carte présentée, en attente d'enrôlement.
	//
	// Elle vit ici et non dans le dépôt des moyens tant que l'issue
	// n'est pas connue : PayZen ne crée l'alias qu'après une
	// autorisation acceptée — « L'alias (token) ne sera pas créé si la
	// demande d'autorisation ou de renseignement est refusée ». La
	// publier dès la création exposerait, le temps que le porteur
	// paie, un alias que le vrai n'a pas encore attribué.
	//
	// Effacée au moment de l'enrôlement : elle n'a plus de raison
	// d'être dupliquée une fois le PaymentMethod créé.
	Card *Card

	// Customer et Metadata sont le contexte marchand, restitués tels
	// quels dans le webhook. Paysim ne les interprète jamais.
	Customer Customer
	Metadata map[string]string

	// Payment porte l'état et le journal d'événements. C'est lui la
	// source de vérité de l'état, pas un champ de cette struct — le
	// domaine seul décide des transitions permises.
	Payment *domain.Payment

	// ReturnURL et NotificationURL sont les cibles du retour navigateur
	// et de l'IPN. Extensions Paysim : un vrai PayZen les tient de la
	// configuration de boutique, pas de la requête. Vides, les
	// endpoints de simulation retombent sur PAYSIM_CALLBACK_URL.
	ReturnURL       string
	NotificationURL string

	// CreatedAt et UpdatedAt en UTC, sur l'enveloppe transaction — les
	// dates du domaine vivent dans le journal d'événements.
	CreatedAt time.Time
	UpdatedAt time.Time

	// PaymentMethodToken pointe le moyen de paiement enregistré associé
	// à cette transaction. Non vide dans deux cas :
	//   - formAction REGISTER_PAY / ASK_REGISTER_PAY : Paysim a généré
	//     un token lors du premier paiement et l'a stocké.
	//   - rejeu one-click : la transaction a été créée à partir d'un
	//     token existant fourni par le marchand.
	// Vide sur un paiement one-shot sans enrôlement.
	PaymentMethodToken string

	// DeclineCode et DeclineMessage conservent le motif bancaire du
	// refus — le code de retour d'autorisation ISO 8583 et son libellé.
	//
	// Le motif partait jusqu'ici dans le kr-answer et dans la note de
	// l'événement, où il finissait aplati en une phrase. Il n'était donc
	// exploitable ni par l'interface, ni par un marchand qui interroge
	// l'API de contrôle, alors que c'est lui qui décide d'une
	// reconduction : un 51 se retente, un 43 non.
	//
	// Conservé ici plutôt que dans le domaine : un code d'acquéreur est
	// du vocabulaire de protocole, et Stripe apportera le sien. La note
	// de l'événement reste inchangée, elle sert la chronologie.
	//
	// Vides quand le refus n'a pas de motif bancaire — abandon,
	// expiration — auquel cas il n'y a rien à afficher.
	DeclineCode    string
	DeclineMessage string
}

// UpdatePaymentRequest est le corps de POST /api-payment/V4/Charge/UpdatePayment.
// Met a jour le contexte d'un formulaire deja cree (typiquement les
// coordonnees du client apres modification cote UI). Ne change pas
// l'etat du domain.Payment associe.
type UpdatePaymentRequest struct {
	// FormToken désigne le paiement à mettre à jour.
	FormToken string `json:"formToken"`

	// Customer et Metadata remplacent le contexte marchand. L'état du
	// paiement n'est pas touché : cet endpoint corrige des
	// coordonnées, il ne fait pas avancer la machine à états.
	Customer Customer          `json:"customer,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// UpdatePaymentAnswer est le contenu de answer sur succes — le meme
// formToken (inchange), pour permettre au marchand de continuer sans
// nouvelle initialisation cote SmartForm.
type UpdatePaymentAnswer struct {
	// FormToken est inchangé : mettre à jour le contexte ne crée pas
	// un nouveau paiement, le marchand garde le même jeton.
	FormToken string `json:"formToken"`
}

// CreateSubscriptionRequest est le corps de POST
// /api-payment/V4/Charge/CreateSubscription. Cree un abonnement
// recurrent lie a un paymentMethodToken deja obtenu (typiquement
// issu d'un formulaire REGISTER_PAY precedent).
//
// EffectDate et Rrule reprennent le vocabulaire iCalendar (RFC 5545) —
// c'est ce que PayZen expose, on recopie tel quel.
type CreateSubscriptionRequest struct {
	// OrderID est la référence marchand de l'abonnement.
	OrderID string `json:"orderId,omitempty"`

	// Amount est le montant d'une échéance en centimes, Currency sa
	// devise ISO 4217.
	Amount   format.Amount `json:"amount"`
	Currency string        `json:"currency"`

	// PaymentMethodToken désigne le moyen enrôlé à prélever.
	PaymentMethodToken string `json:"paymentMethodToken"`

	// EffectDate est la date de première échéance (ISO 8601), Rrule la
	// règle de récurrence RFC 5545, par exemple une fréquence mensuelle
	// d'intervalle 1. Paysim les stocke et les restitue sans jamais les
	// dérouler.
	EffectDate string `json:"effectDate"`
	Rrule      string `json:"rrule"`

	// Metadata libre du marchand.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// CreateSubscriptionAnswer est le contenu de answer sur succes.
type CreateSubscriptionAnswer struct {
	// SubscriptionID est l'identifiant assigné, à passer ensuite aux
	// endpoints de facturation et d'annulation.
	SubscriptionID string `json:"subscriptionId"`
}

// SubscriptionGetRequest est le corps de POST /api-payment/V4/Subscription/Get.
// PayZen exige a la fois subscriptionId et paymentMethodToken —
// double index qui rend la requete moins ambigue.
type SubscriptionGetRequest struct {
	// SubscriptionID désigne l'abonnement à relire.
	SubscriptionID string `json:"subscriptionId"`

	// PaymentMethodToken est requis, comme chez PayZen, et doit
	// correspondre au moyen prélevé par l'abonnement. L'identifiant
	// suffirait techniquement à retrouver l'enregistrement — c'est
	// justement pour ça que Paysim l'exige : accepter ce que le vrai
	// refuse laisse passer une intégration qui échouera en production.
	PaymentMethodToken string `json:"paymentMethodToken"`
}

// SubscriptionGetAnswer est le resume d'un abonnement retourne au marchand.
// Miroir simplifie de ce que PayZen renvoie — on ne modelise pas les
// occurrences de facturation (renewals) en phase 1.
type SubscriptionGetAnswer struct {
	// SubscriptionID identifie l'abonnement, OrderID le rattache à une
	// commande marchand.
	SubscriptionID string `json:"subscriptionId"`
	OrderID        string `json:"orderId,omitempty"`

	// Amount est le montant d'une échéance en centimes, Currency sa
	// devise ISO 4217.
	Amount   format.Amount `json:"amount"`
	Currency string        `json:"currency"`

	// EffectDate et Rrule sont l'échéancier déclaré, restitué tel quel
	// et jamais interprété.
	EffectDate string `json:"effectDate"`
	Rrule      string `json:"rrule"`

	// PaymentMethodToken désigne le moyen prélevé à chaque échéance.
	PaymentMethodToken string `json:"paymentMethodToken"`

	// CreationDate en ISO 8601 UTC.
	CreationDate string `json:"creationDate"`

	// Metadata est la map libre du marchand.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Subscription est le contexte d'un abonnement simule cote Paysim.
// Le champ Cancelled marque une annulation manuelle (via /cancel) ;
// une subscription annulée refuse tout trigger-billing ultérieur.
// Aucun moteur d'échéancier RRule ne tourne en fond — les renewals
// sont déclenchés explicitement par appel à /trigger-billing, choix
// de conception cohérent avec un simulateur (déterminisme total).
type Subscription struct {
	// ID est le subscriptionId assigné par Paysim.
	ID string
	// Brand est la marque Lyra de l'abonnement, héritée par ses
	// échéances. Vide vaut celle de l'instance.
	Brand string


	// OrderID est la référence marchand de l'abonnement.
	OrderID string

	// Amount est le montant d'une échéance en centimes entiers,
	// Currency sa devise ISO 4217. Chaque échéance peut différer du
	// montant déclaré ici — c'est le cas d'un abonnement au prorata.
	Amount   format.Amount
	Currency string

	// PaymentMethodToken désigne le moyen prélevé. L'abonnement ne le
	// possède pas : le révoquer fait échouer les échéances sans
	// annuler l'abonnement lui-même.
	PaymentMethodToken string

	// EffectDate et Rrule décrivent l'échéancier déclaré. Conservés et
	// restitués tels quels, jamais consommés : aucun moteur ne tourne
	// en fond, chaque échéance est déclenchée explicitement. Un
	// scheduler caché ruinerait le déterminisme d'une suite de tests.
	EffectDate string
	Rrule      string

	// Metadata est la map libre du marchand. Elle est recopiée sur
	// chaque Transaction d'échéance, enrichie de subscriptionId — c'est
	// ce lien qui rattache un paiement à son abonnement, sans table
	// dédiée.
	Metadata map[string]string

	// CreatedAt en UTC.
	CreatedAt time.Time

	// Cancelled est définitif : les échéances suivantes sont refusées.
	Cancelled bool
}

// Outcomes supportes pour les APIs de controle
// /paysim/simulate/browserReturn et /paysim/simulate/ipn.
//
// ABANDONED est mappe sur domain.Expire() : semantiquement l'utilisateur
// est parti, le paiement n'a jamais reussi — cf. le status EXPIRED cote
// domaine (pas de nouvel etat "abandoned" au domain, ce serait de la
// dette pour une nuance metier faible).
const (
	OutcomePaid       = "PAID"
	OutcomeAuthorised = "AUTHORISED"
	OutcomeUnpaid     = "UNPAID"
	OutcomeExpired    = "EXPIRED"
	OutcomeAbandoned  = "ABANDONED"
)

// WebhookChaos regroupe les injections de pannes appliquees au webhook
// resultant de la simulation. Chaque flag est independant, tous
// inertes par defaut (invariant 5).
type WebhookChaos struct {
	// Duplicate : le meme webhook est enqueue deux fois — le marchand
	// doit gerer l'idempotence.
	Duplicate bool `json:"duplicate,omitempty"`

	// BadSignature : le kr-hash envoye est altere. Le marchand qui
	// verifie la signature doit refuser le webhook.
	BadSignature bool `json:"badSignature,omitempty"`

	// RaceBeforeResponse : la reponse HTTP au simulate est retardee
	// de 500 ms, laissant le temps au webhook de partir en avance.
	// Cote client, la webhook arrive avant la reponse a l'appel de
	// simulation — la course la plus dure a reproduire en prod.
	RaceBeforeResponse bool `json:"raceBeforeResponse,omitempty"`
}

// BrowserReturnRequest est le corps de POST /paysim/simulate/browserReturn.
// Endpoint de controle Paysim (pas PayZen) : le marchand demande a
// Paysim de simuler la fin d'un parcours et l'envoi du retour signe
// vers son URL. Les champs optionnels ont des valeurs par defaut
// coherentes avec un paiement CB VISA reussi 3DS SUCCESS.
//
// DeliveryDelayMs retarde l'envoi du webhook resultant. Compose avec
// deux appels successifs pour simuler du out-of-order sans flag dedie.
type BrowserReturnRequest struct {
	// FormToken désigne la transaction à faire avancer, tel que rendu
	// par CreatePayment.
	FormToken string `json:"formToken"`

	// ReturnURL surcharge celle de la transaction. Sans elle ni celle
	// de la transaction, le simulateur retombe sur
	// PAYSIM_CALLBACK_URL et le signale par un warn.
	ReturnURL string `json:"returnUrl,omitempty"`

	// Outcome est l'issue à jouer : PAID, AUTHORISED, UNPAID, EXPIRED,
	// ABANDONED. Toute autre valeur est refusée avec la liste des
	// valeurs acceptées — un intégrateur ne doit pas avoir à lire le
	// code du simulateur pour la trouver.
	Outcome string `json:"outcome"`

	// PaymentMethodType et CardBrand décrivent le moyen annoncé dans le
	// webhook. Défauts CARDS et VISA. Ignorés quand un moyen enrôlé
	// existe : ce qu'on annonce vient alors de la carte réelle.
	PaymentMethodType string `json:"paymentMethodType,omitempty"`
	CardBrand         string `json:"cardBrand,omitempty"`

	// Wallet renseigne le portefeuille employé (APPLE_PAY, GOOGLEPAY).
	Wallet string `json:"wallet,omitempty"`

	// ThreeDSStatus pilote le résultat d'authentification annoncé :
	// SUCCESS par défaut, CHALLENGE pour un parcours avec interaction,
	// FAILURE pour un échec. C'est ce qui permet de tester le verdict
	// d'un enrôlement à 0 €.
	ThreeDSStatus string `json:"threeDSStatus,omitempty"`

	// ErrorCode et ErrorMessage habillent un refus. Sans effet sur une
	// issue acceptée.
	ErrorCode    string `json:"errorCode,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`

	// Chaos et DeliveryDelayMs injectent une panne sur cette livraison
	// précise : doublon, signature invalide, course, ou retard en
	// millisecondes. Jamais actifs par défaut — le chaos s'active
	// toujours explicitement.
	Chaos           WebhookChaos `json:"chaos,omitempty"`
	DeliveryDelayMs int          `json:"deliveryDelayMs,omitempty"`
}

// BrowserReturnResponse est le corps de reponse a l'API de controle.
// Renvoie le hash calcule pour permettre au marchand de le comparer
// dans un test d'integration (diagnostic seulement, pas de secret).
type BrowserReturnResponse struct {
	// Status confirme la prise en compte de la simulation.
	Status string `json:"status"`

	// DeliveryID identifie la livraison déclenchée, pour la retrouver
	// dans l'historique.
	DeliveryID string `json:"deliveryId,omitempty"`

	// KrHash est la signature réellement calculée — retournée même
	// quand le chaos altère celle qui part, pour que le marchand
	// puisse constater l'écart.
	KrHash string `json:"krHash,omitempty"`
}

// IPNRequest est le corps de POST /paysim/simulate/ipn. Meme mecanique
// que BrowserReturnRequest, mais le POST resultant part vers
// NotificationURL (webhook serveur-a-serveur) au lieu de ReturnURL
// (retour navigateur). La distinction est logique : deux endpoints
// distincts pour deux flux distincts cote marchand, meme si le
// contenu du POST est identique.
// Les champs sont ceux de BrowserReturnRequest, à la cible près : ici
// l'IPN part vers une notificationUrl serveur à serveur, là le retour
// suit le navigateur du porteur. Deux canaux distincts pour un même
// kr-answer — c'est ce qui permet de provoquer leur inversion.
type IPNRequest struct {
	// FormToken désigne la transaction à faire avancer.
	FormToken string `json:"formToken"`

	// NotificationURL surcharge celle de la transaction. Absente des
	// deux, le serveur retombe sur PAYSIM_CALLBACK_URL.
	NotificationURL string `json:"notificationUrl,omitempty"`

	// Outcome est l'issue à jouer : PAID, AUTHORISED, UNPAID, EXPIRED,
	// ABANDONED.
	Outcome string `json:"outcome"`

	// PaymentMethodType et CardBrand décrivent le moyen annoncé.
	// Ignorés dès qu'un moyen enrôlé existe.
	PaymentMethodType string `json:"paymentMethodType,omitempty"`
	CardBrand         string `json:"cardBrand,omitempty"`

	// Wallet renseigne le portefeuille employé.
	Wallet string `json:"wallet,omitempty"`

	// ThreeDSStatus pilote le verdict d'authentification annoncé.
	ThreeDSStatus string `json:"threeDSStatus,omitempty"`

	// ErrorCode et ErrorMessage habillent un refus.
	ErrorCode    string `json:"errorCode,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`

	// Chaos et DeliveryDelayMs injectent une panne sur cette livraison.
	// Jamais actifs par défaut.
	Chaos           WebhookChaos `json:"chaos,omitempty"`
	DeliveryDelayMs int          `json:"deliveryDelayMs,omitempty"`
}

// IPNResponse est le corps de reponse — identique en structure a
// BrowserReturnResponse.
type IPNResponse struct {
	// Status confirme la prise en compte de la simulation.
	Status string `json:"status"`

	// DeliveryID identifie la livraison déclenchée.
	DeliveryID string `json:"deliveryId,omitempty"`

	// KrHash est la signature réellement calculée, même si celle
	// envoyée a été altérée par le chaos.
	KrHash string `json:"krHash,omitempty"`
}

// KrAnswer est la structure JSON sérialisée et envoyée dans le champ
// POST kr-answer du retour navigateur / du webhook IPN. Structure plate
// au top-level (pas de wrapper `{status, answer}` — celui-ci concerne
// uniquement les reponses de l'API REST V4).
//
// Les champs `_type` sont un artefact du sérialiseur Java de PayZen
// (discriminateur pour la deserialisation cote SDK) — on les reproduit
// litteralement (invariant 3).
type KrAnswer struct {
	// ShopID identifie la boutique côté PayZen. Non renseigné par
	// Paysim, qui ne modélise pas la notion de boutique.
	ShopID string `json:"shopId,omitempty"`

	// OrderCycle vaut CLOSED quand plus rien n'est attendu sur la
	// commande, OPEN sur une autorisation en attente de capture. Ne pas
	// le confondre avec OrderStatus : un paiement refusé est CLOSED.
	OrderCycle string `json:"orderCycle"`

	// OrderStatus est le résultat métier de la commande : PAID, UNPAID,
	// EXPIRED, ABANDONED. C'est le champ que le marchand lit pour
	// savoir s'il a été payé.
	OrderStatus string `json:"orderStatus"`

	// ServerDate est l'instant d'émission, ISO 8601 UTC.
	ServerDate string `json:"serverDate"`

	// ServerURL est l'URL publique du serveur émetteur. Vide chez
	// Paysim, qui ne se connaît pas lui-même au moment de signer.
	ServerURL string `json:"serverUrl,omitempty"`

	// ApplicationVersion est la version annoncée par le serveur. Chez
	// Paysim, une constante qui l'identifie comme simulateur.
	ApplicationVersion string `json:"applicationVersion,omitempty"`

	// ApplicationProvider nomme la marque émettrice, avec les valeurs
	// réelles de la plateforme : PAYZEN, NPS pour Systempay, SOGECOM,
	// LBP pour Scellius, LYRA. Le champ manquait, et un marchand qui s'y
	// fie ne trouvait rien.
	//
	// La valeur ne se déduit pas de l'hôte — quatre hôtes distincts
	// annoncent PAYZEN — elle vient donc de la marque du paiement. Aucun
	// risque de se faire passer pour la vraie plateforme :
	// ApplicationVersion annonce juste à côté qu'il s'agit d'un
	// simulateur.
	ApplicationProvider string `json:"applicationProvider,omitempty"`


	// Mode vaut toujours TEST : un simulateur n'a pas de production.
	Mode string `json:"mode"`

	// OrderDetails porte les montants et la référence de commande.
	OrderDetails KrOrderDetails `json:"orderDetails"`

	// Customer restitue le bloc client envoyé à la création — email,
	// reference, coordonnées de facturation.
	Customer Customer `json:"customer,omitempty"`

	// Transactions liste les tentatives de paiement. Paysim en émet
	// toujours exactement une : il ne modélise pas le paiement en
	// plusieurs fois ni le multi-moyens.
	Transactions []KrTransaction `json:"transactions"`

	// SubscriptionID relie le paiement à un abonnement quand il s'agit
	// d'une échéance.
	SubscriptionID string `json:"subscriptionId,omitempty"`

	// Type est le discriminant de structure PayZen — "V4/Payment" ici.
	// Recopié tel quel : c'est une donnée du protocole.
	Type string `json:"_type"`
}

// KrOrderDetails contient les infos de commande cote PayZen.
type KrOrderDetails struct {
	// OrderTotalAmount est le montant demandé, OrderEffectiveAmount
	// celui réellement débité. Ils divergent sur une autorisation, où
	// les fonds sont réservés sans être prélevés : l'effectif vaut
	// alors zéro.
	OrderTotalAmount     format.Amount `json:"orderTotalAmount"`
	OrderEffectiveAmount format.Amount `json:"orderEffectiveAmount"`

	// OrderCurrency en ISO 4217.
	OrderCurrency string `json:"orderCurrency"`

	// Mode vaut TEST — un simulateur n'a pas de production.
	Mode string `json:"mode"`

	// OrderID est la référence de commande du marchand.
	OrderID string `json:"orderId"`

	// Type est le discriminant PayZen, V4/OrderDetails.
	Type string `json:"_type"`
}

// KrTransaction est un element du tableau transactions[]. En phase 1
// on n'a qu'une entree par retour (pas de paiements en plusieurs fois).
type KrTransaction struct {
	// ShopID identifie la boutique. Non renseigné par Paysim.
	ShopID string `json:"shopId,omitempty"`

	// UUID identifie la transaction. C'est la clé de déduplication
	// attendue côté marchand : le chaos duplicate livre deux fois le
	// même webhook, et seul cet UUID permet de s'en apercevoir.
	UUID string `json:"uuid"`

	// Amount en centimes entiers, Currency en ISO 4217.
	Amount   format.Amount `json:"amount"`
	Currency string        `json:"currency"`

	// PaymentMethodType est le moyen employé : CARDS, IP_WIRE… Paysim
	// n'émet le bloc cardDetails que pour CARDS et CB.
	PaymentMethodType string `json:"paymentMethodType"`

	// PaymentMethodToken est l'alias réutilisable, présent après un
	// enrôlement ou sur un rejeu one-click. Absent sur un paiement
	// refusé : un alias annoncé à côté d'un refus laisserait croire
	// qu'il est débitable.
	PaymentMethodToken string `json:"paymentMethodToken,omitempty"`

	// PaymentMethodTokenStatus dit si l'alias est encore utilisable :
	// ACTIVE, ou CANCELLED pour un alias résilié.
	//
	// PayZen le rend à côté du token. Sans lui, un marchand qui relit
	// une transaction ancienne ne peut pas savoir que l'alias qu'elle
	// nomme a été résilié depuis — il le découvrirait au refus du
	// prochain débit.
	//
	// Vide quand il n'y a pas de token : un statut sans alias ne
	// qualifie rien.
	PaymentMethodTokenStatus string `json:"paymentMethodTokenStatus,omitempty"`

	// Status est le résultat de la transaction : PAID ou UNPAID.
	Status string `json:"status"`

	// DetailedStatus précise ce résultat — AUTHORISED, CAPTURED,
	// REFUSED, EXPIRED, ABANDONED. C'est lui qui distingue une
	// autorisation d'une capture, là où Status donne les deux pour PAID.
	DetailedStatus string `json:"detailedStatus"`

	// OperationType vaut DEBIT sur un paiement, CREDIT sur un
	// remboursement. Paysim ne modélise pas encore le second.
	OperationType string `json:"operationType"`

	// CreationDate est l'instant de la transaction, ISO 8601 UTC.
	CreationDate string `json:"creationDate"`

	// ErrorCode et ErrorMessage détaillent un refus côté PSP. Vides sur
	// succès. Préfixe PAYSIM_ pour ne pas se faire passer pour un code
	// PayZen réel.
	ErrorCode    string `json:"errorCode,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`

	// DetailedErrorCode porte le motif bancaire : le code de retour
	// d'autorisation ISO 8583 que l'acquéreur remonte — 51 pour une
	// provision insuffisante, 43 pour une opposition, 91 pour un
	// émetteur injoignable.
	//
	// Contrairement à ErrorCode, celui-ci n'est pas préfixé : ce n'est
	// pas une valeur Paysim, c'est la norme, et c'est sur elle qu'un
	// marchand écrit sa logique de reconduction. La reproduire à
	// l'identique est le seul moyen qu'un mapping écrit contre Paysim
	// reste valable en production.
	DetailedErrorCode    string `json:"detailedErrorCode,omitempty"`
	DetailedErrorMessage string `json:"detailedErrorMessage,omitempty"`

	// Metadata restitue la map libre envoyée à la création. C'est le
	// canal prévu pour rattacher un paiement à un objet métier sans
	// dépendre de l'orderId.
	Metadata map[string]string `json:"metadata,omitempty"`

	// TransactionDetails porte la carte, le 3DS et le contexte.
	TransactionDetails KrTransactionDetails `json:"transactionDetails"`

	// Type est le discriminant PayZen — "V4/PaymentTransaction".
	Type string `json:"_type"`
}

// KrTransactionDetails porte les details specifiques a la methode de
// paiement utilisee (CB, wallet, virement...). CardDetails et
// ThreeDSResponse sont des pointeurs pour permettre leur omission
// naturelle (omitempty) quand la methode ne les porte pas.
type KrTransactionDetails struct {
	// Mid est le numéro de contrat commerçant chez l'acquéreur. Non
	// renseigné par Paysim, qui ne modélise pas l'acquisition.
	Mid string `json:"mid,omitempty"`

	// CreationContext dit d'où vient la transaction. Paysim annonce
	// toujours CHARGE, y compris sur une échéance d'abonnement — un
	// vrai PSP distinguerait le contexte récurrent.
	CreationContext string `json:"creationContext"`

	// Wallet nomme le portefeuille employé, quand il y en a un.
	Wallet string `json:"wallet,omitempty"`

	// CardDetails n'est présent que pour les moyens de type carte. Un
	// virement ou un wallet porteraient d'autres blocs, non modélisés.
	CardDetails *KrCardDetails `json:"cardDetails,omitempty"`

	// ThreeDSResponse porte le verdict d'authentification du porteur.
	ThreeDSResponse *KrThreeDSResponse `json:"threeDSResponse,omitempty"`

	// Type est le discriminant PayZen, V4/TransactionDetails.
	Type string `json:"_type"`
}

// KrCardDetails contient les infos carte visibles cote marchand.
// PAN toujours masque — la vraie PAN n'entre jamais dans un retour PSP,
// invariant PCI-DSS. Le simulateur reproduit ce masquage tel quel.
type KrCardDetails struct {
	// PAN est toujours masqué — jamais le numéro complet, même si le
	// simulateur le stocke en clair de son côté.
	PAN string `json:"pan"`

	// Brand est la marque déclarée, EffectiveBrand celle retenue après
	// arbitrage. PayZen les distingue pour les cartes co-badgées, où
	// le porteur choisit son réseau ; Paysim renvoie la même valeur
	// dans les deux, faute de modéliser ce choix.
	Brand          string `json:"brand"`
	EffectiveBrand string `json:"effectiveBrand,omitempty"`

	// HolderName est le nom du porteur. Absent quand l'enrôlement ne
	// l'a pas fourni — un wallet n'en transmet pas.
	HolderName string `json:"holderName,omitempty"`

	// ProductCategory distingue CREDIT, DEBIT et PREPAID. Les règles
	// d'autorisation en dépendent côté marchand.
	ProductCategory string `json:"productCategory,omitempty"`

	// ExpiryMonth (1-12) et ExpiryYear (4 chiffres).
	ExpiryMonth int `json:"expiryMonth"`
	ExpiryYear  int `json:"expiryYear"`

	// Country est le pays émetteur en ISO 3166-1 alpha-2, IssuerName
	// la banque. C'est ce qui rend testable le refus géographique ou
	// le routage par établissement.
	Country    string `json:"country,omitempty"`
	IssuerName string `json:"issuerName,omitempty"`

	// Type est le discriminant PayZen — "V4/CardDetails".
	Type string `json:"_type"`
}

// KrThreeDSResponse porte le resultat de l'authentification 3D Secure
// telle qu'elle a ete portee par le SmartForm cote client.
type KrThreeDSResponse struct {
	// AuthenticationResultData porte le verdict proprement dit. Le
	// niveau d'imbrication vient de PayZen et se recopie tel quel.
	AuthenticationResultData KrAuthenticationResultData `json:"authenticationResultData"`

	// Type est le discriminant PayZen, V4/ThreeDSResponse.
	Type string `json:"_type"`
}

// KrAuthenticationResultData porte le status precis du 3DS.
// Status : SUCCESS | FAILURE | NOT_ENROLLED | UNAVAILABLE.
// AuthenticationType : FRICTIONLESS | CHALLENGE.
type KrAuthenticationResultData struct {
	// Status est le verdict d'authentification : SUCCESS, FAILURE,
	// NOT_ENROLLED, UNAVAILABLE. C'est lui que lit un marchand pour
	// décider s'il accepte la transaction.
	Status string `json:"status"`

	// AuthenticationType dit comment le porteur a été authentifié :
	// FRICTIONLESS sans interaction, CHALLENGE avec. Paysim le déduit
	// du statut demandé.
	AuthenticationType string `json:"authenticationType,omitempty"`

	// Type est le discriminant PayZen, V4/AuthenticationResultData.
	Type string `json:"_type"`
}
