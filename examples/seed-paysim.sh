#!/usr/bin/env bash
# examples/seed-paysim.sh — peuple un Paysim de démo avec un jeu de
# paiements, subscriptions et moyens de paiement varié, pour voir
# les états visuels de l'UI (captured / declined / actif / révoqué /
# expiré). Utile en première prise en main : « lance ça et regarde ».
#
# Prérequis :
#   - Paysim tourne sur http://localhost:30880. Surcharger PAYSIM_URL
#     si Paysim est ailleurs.
#   - PAYSIM_PAYZEN_HMAC_KEY configuré côté serveur (sinon simulate
#     retourne 400 sur les cartes).
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
CURL_HEADERS=(-H 'Content-Type: application/json')

if [[ "${1:-}" == "--purge" ]]; then
    echo "==> Purge des paiements existants"
    curl -sX DELETE "$API/payments" >/dev/null
    echo "  purgés"
fi

# Extraction d'un champ scalaire d'un objet JSON, en shell pur —
# suffisant pour les réponses simples de l'API Paysim (uuid, id, token).
# Pour parser un vrai JSON, préférer jq ; ici on évite la dépendance.
json_get() {
    local field="$1"
    grep -o "\"$field\":\"[^\"]*\"" | head -1 | sed "s/^\"$field\":\"//;s/\"\$//"
}

echo "==> 1. Paiement nominal (captured)"
p=$(curl -s -X POST "$API/payments" "${CURL_HEADERS[@]}" \
    -d '{"amount":4990,"currency":"EUR","orderId":"ORDER-NOMINAL-001"}')
uuid=$(echo "$p" | json_get uuid)
curl -s -X POST "$API/payments/$uuid/simulate" "${CURL_HEADERS[@]}" \
    -d "{\"outcome\":\"PAID\",\"channel\":\"ipn\",\"notificationUrl\":\"$NOTIF_URL\"}" >/dev/null
echo "  $uuid — captured"

echo "==> 2. Paiement refus magic amount (xxx01 → UNPAID)"
p=$(curl -s -X POST "$API/payments" "${CURL_HEADERS[@]}" \
    -d '{"amount":1001,"currency":"EUR","orderId":"ORDER-MAGIC-AMOUNT"}')
uuid=$(echo "$p" | json_get uuid)
curl -s -X POST "$API/payments/$uuid/simulate" "${CURL_HEADERS[@]}" \
    -d "{\"outcome\":\"PAID\",\"channel\":\"ipn\",\"notificationUrl\":\"$NOTIF_URL\"}" >/dev/null
echo "  $uuid — declined (magic amount)"

echo "==> 3. Paiement autorisé (fonds réservés, non débités)"
p=$(curl -s -X POST "$API/payments" "${CURL_HEADERS[@]}" \
    -d '{"amount":7500,"currency":"EUR","orderId":"ORDER-AUTH-ONLY"}')
uuid=$(echo "$p" | json_get uuid)
curl -s -X POST "$API/payments/$uuid/simulate" "${CURL_HEADERS[@]}" \
    -d "{\"outcome\":\"AUTHORISED\",\"channel\":\"ipn\",\"notificationUrl\":\"$NOTIF_URL\"}" >/dev/null
echo "  $uuid — authorized"

echo "==> 4. Enrolement Visa valide (long-terme, dates 2028)"
p=$(curl -s -X POST "$API/payments" "${CURL_HEADERS[@]}" \
    -d '{"amount":2990,"currency":"EUR","orderId":"SUB-INIT-VISA","formAction":"REGISTER_PAY","card":{"pan":"4111111111111111","expiryMonth":12,"expiryYear":2028,"brand":"VISA"}}')
uuid=$(echo "$p" | json_get uuid)
token_visa=$(echo "$p" | json_get paymentMethodToken)
curl -s -X POST "$API/payments/$uuid/simulate" "${CURL_HEADERS[@]}" \
    -d "{\"outcome\":\"PAID\",\"channel\":\"ipn\",\"notificationUrl\":\"$NOTIF_URL\"}" >/dev/null
echo "  $uuid — captured, token Visa = $token_visa"

echo "==> 5. Subscription mensuelle active + 2 renewals réussis"
s=$(curl -s -X POST "$API/subscriptions" "${CURL_HEADERS[@]}" \
    -d "{\"paymentMethodToken\":\"$token_visa\",\"amount\":2990,\"currency\":\"EUR\",\"orderId\":\"SUB-42-PRO\",\"effectDate\":\"2026-09-01\",\"rrule\":\"RRULE:FREQ=MONTHLY;INTERVAL=1\",\"metadata\":{\"plan\":\"pro\"}}")
subid=$(echo "$s" | json_get id)
echo "  subscription $subid"
for i in 1 2; do
    curl -s -X POST "$API/subscriptions/$subid/trigger-billing" >/dev/null
    echo "  renewal $i triggered"
done

echo "==> 6. Enrolement magic PAN Visa 4000...02 (refus systématique)"
p=$(curl -s -X POST "$API/payments" "${CURL_HEADERS[@]}" \
    -d '{"amount":1500,"currency":"EUR","orderId":"CARD-MAGIC-DECLINE","formAction":"REGISTER_PAY","card":{"pan":"4000000000000002","expiryMonth":12,"expiryYear":2028,"brand":"VISA"}}')
token_magic=$(echo "$p" | json_get paymentMethodToken)
uuid=$(echo "$p" | json_get uuid)
curl -s -X POST "$API/payments/$uuid/simulate" "${CURL_HEADERS[@]}" \
    -d "{\"outcome\":\"PAID\",\"channel\":\"ipn\",\"notificationUrl\":\"$NOTIF_URL\"}" >/dev/null
echo "  $uuid — declined (magic PAN au simulate), token = $token_magic"

echo "==> 7. Subscription sur CB magic PAN → renewal declined"
s=$(curl -s -X POST "$API/subscriptions" "${CURL_HEADERS[@]}" \
    -d "{\"paymentMethodToken\":\"$token_magic\",\"amount\":990,\"currency\":\"EUR\",\"orderId\":\"SUB-FAILING\",\"rrule\":\"RRULE:FREQ=MONTHLY\"}")
subid=$(echo "$s" | json_get id)
curl -s -X POST "$API/subscriptions/$subid/trigger-billing" >/dev/null
echo "  subscription $subid + renewal (declined)"

echo "==> 8. Enrolement Mastercard valide"
p=$(curl -s -X POST "$API/payments" "${CURL_HEADERS[@]}" \
    -d '{"amount":3500,"currency":"EUR","orderId":"MC-CHECKOUT","formAction":"REGISTER_PAY","card":{"pan":"5555555555554444","expiryMonth":6,"expiryYear":2029}}')
uuid=$(echo "$p" | json_get uuid)
curl -s -X POST "$API/payments/$uuid/simulate" "${CURL_HEADERS[@]}" \
    -d "{\"outcome\":\"PAID\",\"channel\":\"ipn\",\"notificationUrl\":\"$NOTIF_URL\"}" >/dev/null
echo "  $uuid — captured"

echo "==> 9. Enrolement Amex valide"
p=$(curl -s -X POST "$API/payments" "${CURL_HEADERS[@]}" \
    -d '{"amount":8900,"currency":"EUR","orderId":"AMEX-CHECKOUT","formAction":"REGISTER_PAY","card":{"pan":"371449635398431","expiryMonth":3,"expiryYear":2027}}')
uuid=$(echo "$p" | json_get uuid)
curl -s -X POST "$API/payments/$uuid/simulate" "${CURL_HEADERS[@]}" \
    -d "{\"outcome\":\"PAID\",\"channel\":\"ipn\",\"notificationUrl\":\"$NOTIF_URL\"}" >/dev/null
echo "  $uuid — captured"

echo "==> 10. Enrolement CB expirée (01/2020) — état « Expiré » côté UI"
p=$(curl -s -X POST "$API/payments" "${CURL_HEADERS[@]}" \
    -d '{"amount":1200,"currency":"EUR","orderId":"CARD-EXPIRED","formAction":"REGISTER_PAY","card":{"pan":"4242424242424242","expiryMonth":1,"expiryYear":2020,"brand":"VISA"}}')
uuid=$(echo "$p" | json_get uuid)
token_exp=$(echo "$p" | json_get paymentMethodToken)
curl -s -X POST "$API/payments/$uuid/simulate" "${CURL_HEADERS[@]}" \
    -d "{\"outcome\":\"PAID\",\"channel\":\"ipn\",\"notificationUrl\":\"$NOTIF_URL\"}" >/dev/null
echo "  $uuid — declined (CB expirée au simulate), token = $token_exp"

echo "==> 11. Enrolement Mastercard série 2 (nouveau BIN) puis révocation"
p=$(curl -s -X POST "$API/payments" "${CURL_HEADERS[@]}" \
    -d '{"amount":2200,"currency":"EUR","orderId":"MC2-CHECKOUT","formAction":"REGISTER_PAY","card":{"pan":"2223000048400011","expiryMonth":10,"expiryYear":2030}}')
uuid=$(echo "$p" | json_get uuid)
token_mc2=$(echo "$p" | json_get paymentMethodToken)
curl -s -X POST "$API/payments/$uuid/simulate" "${CURL_HEADERS[@]}" \
    -d "{\"outcome\":\"PAID\",\"channel\":\"ipn\",\"notificationUrl\":\"$NOTIF_URL\"}" >/dev/null
curl -sX POST "$API/payment-methods/$token_mc2/revoke" -o /dev/null
echo "  $uuid — captured + moyen révoqué manuellement"

# Comptage : chaque entrée du tableau porte le champ scalaire attendu
# une seule fois — grep -c compte les occurrences sans dépendance JSON.
count_field() {
    curl -s "$1" | grep -o "\"$2\":" | wc -l | tr -d ' '
}

echo ""
echo "==> Résumé"
echo "Paiements: $(count_field "$API/payments" uuid)"
echo "Subscriptions: $(count_field "$API/subscriptions" id)"
echo "Payment methods: $(count_field "$API/payment-methods" token)"
echo ""
echo "UI : ${PAYSIM_URL:-http://localhost:30880}/"
