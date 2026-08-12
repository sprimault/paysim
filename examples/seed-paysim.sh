#!/usr/bin/env bash
# examples/seed-paysim.sh — peuple un Paysim déjà lancé avec le jeu de
# démonstration : moyens de paiement dans chacun de leurs états,
# abonnements avec et sans échéance, paiements couvrant les états de la
# machine. Utile en première prise en main : « lance ça et regarde ».
#
# Le jeu produit est identique à celui de demo-ui.sh, qui monte en plus
# le conteneur et son récepteur de webhooks. Il tient en deux parties :
# des cas remarquables, chacun porteur d'une chose à voir, puis du
# volume — trente paiements répartis sur les états. Ce volume n'est pas
# décoratif : la recherche, les filtres d'état, la pagination et
# l'en-tête collant ne se jugent pas sur huit lignes.
#
# Prérequis :
#   - Paysim tourne sur http://localhost:30880. Surcharger PAYSIM_URL
#     si Paysim est ailleurs.
#   - PAYSIM_PAYZEN_HMAC_KEY et PAYSIM_PAYZEN_REST_PASSWORD configurés
#     côté serveur (sinon simulate retourne 400 sur les cartes).
#   - bash + curl + grep -o. Aucune autre dépendance.
#
# Variables :
#   PAYSIM_URL   URL de base de Paysim (défaut http://localhost:30880).
#   NOTIF_URL    URL des webhooks (défaut http://localhost:1/discard —
#                port fermé, échec immédiat, pas de DNS lookup lent).
#
# Flags :
#   --purge   Vide les paiements existants avant de peupler. Les
#             subscriptions et moyens de paiement ne sont pas purgés
#             (pas d'endpoint bulk delete pour ces ressources en v1).
#
# Non idempotent : les orderId ne sont pas uniques et les entrées
# s'ajoutent à chaque exécution. Utiliser --purge pour repartir propre
# côté paiements, ou supprimer le volume SQLite pour tout raser.

set -euo pipefail

API="${PAYSIM_URL:-http://localhost:30880}/paysim/api/v1"
NOTIF_URL="${NOTIF_URL:-http://localhost:1/discard}"

if [[ "${1:-}" == "--purge" ]]; then
    echo "==> Purge des paiements existants"
    curl -sX DELETE "$API/payments" >/dev/null
    echo "  purgés"
fi

post() { curl -s -X POST "$API$1" -H 'Content-Type: application/json' -d "${2:-}"; }

# Extraction d'un champ scalaire d'un objet JSON, en shell pur —
# suffisant pour les réponses simples de l'API Paysim (uuid, id, token).
# Pour parser un vrai JSON, préférer jq ; ici on évite la dépendance.
field() { grep -o "\"$1\":\"[^\"]*\"" | head -1 | sed "s/.*\":\"//; s/\"$//"; }

# Joue l'issue d'un paiement en attente, webhook compris.
simulate() {
    post "/payments/$1/simulate" \
        "{\"outcome\":\"${2:-PAID}\",\"channel\":\"ipn\",\"notificationUrl\":\"$NOTIF_URL\"}" >/dev/null
}

# Enrôle une carte sans rien débiter et rend l'alias créé.
enrole() {
    post /payments "{\"amount\":0,\"currency\":\"EUR\",\"orderId\":\"$1\",
      \"formAction\":\"REGISTER\",\"card\":$2}" | field paymentMethodToken
}

# Renvoie N fois la dernière livraison d'un paiement.
#
# L'identifiant n'est lu qu'une fois : rejouer un rejeu produit la même
# chose, et l'identifiant ne s'empile plus depuis qu'il repart de la
# livraison d'origine. La pause laisse la livraison se terminer — c'est
# à ce moment-là qu'elle entre dans l'historique, donc dans le compte.
rejouer() {
    local livraison
    livraison=$(curl -s "$API/webhooks?paymentUuid=$1" | field id)
    [ -n "$livraison" ] || return 0
    local i=0
    while [ "$i" -lt "$2" ]; do
        post "/webhooks/$livraison/replay" >/dev/null
        sleep 1
        i=$((i + 1))
    done
}

echo "==> 1. Enrolement portant un contexte client complet"
T1=$(post /payments '{
  "amount": 0, "currency": "EUR", "orderId": "REGISTER-2041",
  "formAction": "REGISTER",
  "customer": {
    "email": "alice.martin@example.com",
    "reference": "client-4821",
    "billingDetails": {
      "title": "Mme", "language": "fr",
      "firstName": "Alice", "lastName": "MARTIN",
      "address": "1 rue de la Paix", "zipCode": "75002",
      "city": "Paris", "country": "FR"
    },
    "shippingDetails": {
      "category": "COMPANY", "legalName": "ACME SARL",
      "firstName": "Bob", "lastName": "DURAND",
      "phoneNumber": "+33600000000",
      "address": "avenue des Champs", "zipCode": "75008",
      "city": "Paris", "country": "FR"
    },
    "extraDetails": {"ipAddress": "203.0.113.7", "fingerPrintId": "fp-9f2c1ab4"}
  },
  "metadata": {"plan": "pro", "source": "onboarding"},
  "card": {"pan": "5555555555554444", "expiryMonth": 12, "expiryYear": 2030,
           "holderName": "ALICE MARTIN", "productCategory": "DEBIT"}
}' | field paymentMethodToken)
echo "  token Mastercard = $T1"

echo "==> 2. Enrolements Visa et Amex"
T2=$(enrole REGISTER-2042 '{"pan":"4111111111111111","expiryMonth":6,"expiryYear":2028}')
T5=$(enrole REGISTER-2045 '{"pan":"371449635398431","expiryMonth":3,"expiryYear":2029}')
T6=$(enrole REGISTER-2046 '{"pan":"4111111111111111","expiryMonth":9,"expiryYear":2031}')
echo "  tokens actifs = $T2, $T5, $T6"

echo "==> 3. Moyen périmé — état « Expiré » côté UI"
# Une carte ne s'enrôle jamais déjà expirée : l'autorisation serait
# refusée et aucun alias ne naîtrait. On l'enregistre saine, puis on la
# fait vieillir — c'est le cas réel, et le seul qui produise un alias
# périmé à regarder dans l'interface.
T3=$(enrole REGISTER-2043 '{"pan":"4242424242424242","expiryMonth":12,"expiryYear":2030}')
post "/payment-methods/$T3/expire" >/dev/null
echo "  token = $T3 — périmé"

echo "==> 4. Paiement nominal (captured)"
U=$(post /payments '{"amount":4990,"currency":"EUR","orderId":"CMD-1042",
  "customer":{"email":"bob@example.com","reference":"client-1042"}}' | field uuid)
simulate "$U"
U42=$U
echo "  $U — captured"

echo "==> 5. Trois refus, trois motifs"
# Les centimes portent le motif : .01 donne un 51, .02 un 43, .04 un 91.
for pair in "1001:51:CMD-1043" "1002:43:CMD-1046" "1004:91:CMD-1047"; do
    amount=${pair%%:*}; rest=${pair#*:}; code=${rest%%:*}; order=${rest##*:}
    U=$(post /payments "{\"amount\":$amount,\"currency\":\"EUR\",\"orderId\":\"$order\"}" | field uuid)
    simulate "$U"
    echo "  $order — declined ($code)"
done
# Dernier tour de boucle : CMD-1047, gardé pour les rejeux plus bas.
U47=$U

echo "==> 6. Paiement en attente, sans issue jouée"
post /payments '{"amount":12500,"currency":"EUR","orderId":"CMD-1044",
  "customer":{"email":"dave@example.com","reference":"client-1044"}}' >/dev/null
echo "  CMD-1044 — initiated"

echo "==> 7. Paiement autorisé (fonds réservés, non débités)"
U=$(post /payments '{"amount":7500,"currency":"EUR","orderId":"CMD-1048",
  "customer":{"email":"erin@example.com","reference":"client-1048"}}' | field uuid)
simulate "$U" AUTHORISED
echo "  $U — authorized"

echo "==> 8. Abonnement mensuel actif + 2 échéances jouées"
S1=$(post /subscriptions "{\"paymentMethodToken\":\"$T1\",\"amount\":2990,
  \"currency\":\"EUR\",\"orderId\":\"SUB-77\",\"effectDate\":\"2026-09-01T00:00:00Z\",
  \"rrule\":\"RRULE:FREQ=MONTHLY;INTERVAL=1\",\"metadata\":{\"plan\":\"pro\"}}" | field id)
post "/subscriptions/$S1/trigger-billing" >/dev/null
post "/subscriptions/$S1/trigger-billing" >/dev/null
echo "  $S1 — SUB-77, 2 échéances"

echo "==> 9. Moyen révoqué, puis échéance refusée"
# Refus sans motif bancaire : ce n'est pas un émetteur qui refuse, c'est
# le moyen qui n'est plus utilisable. À comparer avec SUB-81 plus bas,
# qui refuse pour provision et porte un code 51.
T4=$(enrole REGISTER-2044 '{"pan":"2223000048400011","expiryMonth":10,"expiryYear":2030}')
S2=$(post /subscriptions "{\"paymentMethodToken\":\"$T4\",\"amount\":990,
  \"currency\":\"EUR\",\"orderId\":\"SUB-78\",\"rrule\":\"RRULE:FREQ=MONTHLY\"}" | field id)
post "/payment-methods/$T4/revoke" >/dev/null
post "/subscriptions/$S2/trigger-billing" >/dev/null
echo "  token $T4 révoqué, $S2 — SUB-78 échéance refusée"

echo "==> 10. Abonnement annulé, et abonnement sans échéance"
S3=$(post /subscriptions "{\"paymentMethodToken\":\"$T6\",\"amount\":4900,
  \"currency\":\"EUR\",\"orderId\":\"SUB-79\",\"rrule\":\"RRULE:FREQ=YEARLY\"}" | field id)
post "/subscriptions/$S3/cancel" >/dev/null
post /subscriptions "{\"paymentMethodToken\":\"$T2\",\"amount\":1490,
  \"currency\":\"EUR\",\"orderId\":\"SUB-80\",\"rrule\":\"RRULE:FREQ=WEEKLY\"}" >/dev/null
echo "  SUB-79 annulé, SUB-80 sans échéance"

echo "==> 11. Abonnement dont l'échéance refuse pour provision"
# Le seul levier qui produise ce cas : sur un échéancier le montant est
# imposé, donc le montant magique n'est pas disponible. La carte à
# découvert s'enrôle — une vérification n'engage aucun montant, donc
# n'interroge pas le solde — et ne refuse qu'au débit, avec son code 51.
#
# À comparer avec SUB-78, refusé par révocation et sans code bancaire :
# le marchand ne traite pas les deux de la même façon.
T7=$(enrole REGISTER-2047 '{"pan":"4000000000000002","expiryMonth":12,"expiryYear":2030}')
S4=$(post /subscriptions "{\"paymentMethodToken\":\"$T7\",\"amount\":2490,
  \"currency\":\"EUR\",\"orderId\":\"SUB-81\",\"rrule\":\"RRULE:FREQ=MONTHLY\"}" | field id)
post "/subscriptions/$S4/trigger-billing" >/dev/null
echo "  $S4 — SUB-81, échéance refusée avec le motif 51"

echo "==> 12. Rejeu one-click sur l'alias enrôlé"
post /payments "{\"amount\":1990,\"currency\":\"EUR\",\"orderId\":\"CMD-1045\",
  \"paymentMethodToken\":\"$T1\"}" >/dev/null
echo "  CMD-1045"

echo "==> 13. Volume : 30 paiements répartis sur les états"
# Répartis par le rang et non tirés au hasard : deux exécutions donnent
# le même écran, ce qui rend une capture ou une comparaison reproductible.
for i in $(seq 1 30); do
    order=$(printf 'CMD-2%03d' "$i")
    base=$(( (12 + i * 7) * 100 ))
    case $(( i % 5 )) in
        0) case $(( (i / 5) % 3 )) in 0) c=1 ;; 1) c=2 ;; *) c=4 ;; esac
           amount=$(( base + c )); issue=PAID ;;
        1) amount=$base; issue=NONE ;;
        3) amount=$base; issue=AUTHORISED ;;
        *) amount=$(( base + 50 )); issue=PAID ;;
    esac
    U=$(post /payments "{\"amount\":$amount,\"currency\":\"EUR\",\"orderId\":\"$order\",
      \"customer\":{\"email\":\"client$i@example.com\",\"reference\":\"client-2$i\"}}" | field uuid)
    [ "$issue" = NONE ] || simulate "$U" "$issue"
    [ "$i" = 12 ] && U12=$U
done
echo "  CMD-2001 à CMD-2030"

echo "==> 14. Rejeux, pour que la pastille du bouton de renvoi compte"
# Des nombres differents sur trois paiements : sans rejeu, la pastille
# ne s'affiche nulle part et l'ecran ne montre pas ce qu'il sait faire.
rejouer "$U42" 1
rejouer "$U47" 2
rejouer "$U12" 3
echo "  CMD-1042 (1), CMD-1047 (2), CMD-2012 (3)"

# Comptage : chaque entrée du tableau porte le champ scalaire attendu
# une seule fois — grep -o compte les occurrences sans dépendance JSON.
count() { curl -s "$API/$1" | grep -o "\"$2\":" | wc -l | tr -d ' '; }

echo ""
echo "==> Résumé"
echo "Paiements: $(count payments uuid)"
echo "Subscriptions: $(count subscriptions id)"
echo "Payment methods: $(count payment-methods token)"
echo ""
echo "Recherche : taper « client-2 » pour filtrer le volume"
echo "Pastille de rejeux : CMD-1042 (1), CMD-1047 (2), CMD-2012 (3)"
echo "Deux refus d'échéance à comparer : SUB-78 sans code, SUB-81 en 51"
echo "UI : ${PAYSIM_URL:-http://localhost:30880}/"
