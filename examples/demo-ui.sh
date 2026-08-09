#!/usr/bin/env bash
# examples/demo-ui.sh — monte une instance de démonstration complète et
# la peuple, pour regarder l'interface sans rien installer.
#
# Différence avec seed-paysim.sh, qui le complète plutôt qu'il ne le
# remplace : celui-ci suppose un Paysim déjà lancé et se contente de le
# peupler. Ici, tout est monté — Paysim, un récepteur de webhooks, le
# réseau qui les relie — puis détruit d'une commande.
#
# Le jeu de données couvre ce qu'on veut voir d'un coup d'œil : un
# contexte client complet, des refus avec des motifs distincts, un
# paiement en attente, un moyen inexploitable, un abonnement avec une
# échéance jouée, un rejeu en un clic.
#
# Prérequis :
#   - Docker installé ET démarré — sous Windows ou macOS, cela veut dire
#     Docker Desktop lancé, pas seulement installé. Le script a besoin de
#     créer un réseau et de publier un port.
#   - bash, curl, grep. Aucune autre dépendance — pas d'interpréteur
#     JSON, ce qui le rend utilisable tel quel sous git-bash.
#
# Variables :
#   PORT         port publié sur l'hôte (défaut 30880).
#   IMAGE        image à monter (défaut ghcr.io/sprimault/paysim:latest).
#                Passer :edge pour l'état de la branche principale.
#   SINK_IMAGE   image du récepteur de webhooks (défaut traefik/whoami,
#                qui répond 200 à toute requête).
#   HOST_IP      adresse par laquelle le navigateur joint l'hôte. Déduite
#                automatiquement sous Linux ; à fournir ailleurs.
#
# Aucun PAYSIM_STORE n'est passé : la démo tourne sur le mode par défaut
# de l'image, sans état. C'est délibéré — un exemple qui a besoin d'une
# option pour fonctionner cache un défaut au lieu de le montrer.
#
# Non idempotent : relancer recrée tout à zéro, les conteneurs existants
# étant supprimés d'abord.
set -uo pipefail

PORT=${PORT:-30880}
IMAGE=${IMAGE:-ghcr.io/sprimault/paysim:latest}
SINK_IMAGE=${SINK_IMAGE:-traefik/whoami}
NAME=paysim-demo
SINK=paysim-sink
NET=paysim-demo-net

# hostname -I est propre à GNU/Linux : ailleurs, la variable prend le
# relais. Le repli sur localhost reste utilisable depuis la machine même.
IP=${HOST_IP:-$(hostname -I 2>/dev/null | awk '{print $1}')}
IP=${IP:-127.0.0.1}

if ! docker version >/dev/null 2>&1; then
  echo "Docker ne repond pas. Installer Docker, ou demarrer Docker Desktop." >&2
  exit 1
fi

docker rm -f "$NAME" "$SINK" >/dev/null 2>&1
docker network create "$NET" >/dev/null 2>&1

# Le récepteur tourne dans un conteneur, sur le même réseau que Paysim.
# Viser un port de l'hôte depuis un conteneur dépend de règles de
# filtrage qui varient d'une machine à l'autre ; le réseau Docker, non.
#
# Une image qui répond 200 à tout, plutôt qu'un script monté en volume :
# sous git-bash, MSYS réécrit les chemins des arguments et transformait
# la cible du montage en chemin Windows, tuant le conteneur au
# démarrage. Sans fichier, la question ne se pose plus.
docker run -d --name "$SINK" --network "$NET" "$SINK_IMAGE" >/dev/null \
  || { echo "recepteur de webhooks non demarre"; exit 1; }

docker pull "$IMAGE" >/dev/null 2>&1
# PUBLIC_URL est ce que voit le navigateur, CALLBACK_URL ce que vise le
# serveur pour livrer ses webhooks. Deux configurations distinctes, et
# jamais l'une déduite de l'autre : c'est ce qui casse dès qu'un ingress
# entre en jeu.
docker run -d --name "$NAME" --network "$NET" -p "$PORT:8080" \
  -e PAYSIM_PUBLIC_URL="http://$IP:$PORT" \
  -e PAYSIM_CALLBACK_URL="http://$SINK" \
  -e PAYSIM_PAYZEN_HMAC_KEY=demo-hmac-key \
  -e PAYSIM_LOG_LEVEL=warn \
  "$IMAGE" >/dev/null || { echo "docker run echoue"; exit 1; }

for _ in $(seq 1 30); do
  curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1 && break
  sleep 1
done

API="http://127.0.0.1:$PORT/paysim/api/v1"
post() { curl -s -X POST "$API$1" -H 'Content-Type: application/json' -d "$2"; }

# Extraction par grep plutôt que par un interpréteur JSON, comme
# seed-paysim.sh : sous git-bash, `python3` n'est pas Python mais le
# raccourci du Microsoft Store, qui répond « Python est introuvable » sur
# stdout. Les champs lus sont des chaînes plates, un grep suffit — et le
# script ne dépend plus que de bash, curl et grep.
field() { grep -o "\"$1\":\"[^\"]*\"" | head -1 | sed "s/.*\":\"//; s/\"$//"; }

# Compte les éléments d'une collection en s'appuyant sur une clé présente
# une fois par entrée. Approximatif par nature, suffisant pour un
# affichage de fin de script.
count() { curl -s "$API/$1" | grep -o "\"$2\":" | wc -l | tr -d ' '; }

echo "--- jeu de donnees ---"

# Enrolement portant un contexte client complet : c'est celui-ci qui
# remplit toutes les sections du bloc Client.
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
      "identityCode": "12345678900011",
      "firstName": "Bob", "lastName": "DURAND",
      "phoneNumber": "+33600000000",
      "streetNumber": "12", "address": "avenue des Champs",
      "address2": "batiment C", "district": "8e",
      "zipCode": "75008", "city": "Paris", "state": "IDF", "country": "FR",
      "deliveryCompanyName": "TRANSPORTEUR X",
      "shippingSpeed": "EXPRESS", "shippingMethod": "RELAY_POINT"
    },
    "extraDetails": {
      "ipAddress": "203.0.113.7", "fingerPrintId": "fp-9f2c1ab4",
      "browserUserAgent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
      "browserAccept": "text/html,application/xhtml+xml"
    }
  },
  "metadata": {"plan": "pro", "source": "onboarding", "campagne": "ete-2026"},
  "card": {"pan": "5555555555554444", "expiryMonth": 12, "expiryYear": 2030,
           "holderName": "ALICE MARTIN", "country": "FR",
           "productCategory": "DEBIT", "issuerName": "BANQUE DE TEST"}
}' | field paymentMethodToken)
echo "  moyen enrole (contexte complet) : $T1"

U2=$(post /payments '{"amount": 4990, "currency": "EUR", "orderId": "CMD-1042",
  "customer": {"email": "bob@example.com", "reference": "client-1042"}}' | field uuid)
post "/payments/$U2/simulate" '{"outcome":"PAID","channel":"ipn"}' >/dev/null
echo "  paiement capture : CMD-1042"

# Les centimes portent le motif du refus : .01 donne un 51, .02 un 43,
# .04 un 91. Trois motifs pour comparer trois charges utiles distinctes.
for pair in "1001:51:CMD-1043" "1002:43:CMD-1046" "1004:91:CMD-1047"; do
  amount=${pair%%:*}; rest=${pair#*:}; code=${rest%%:*}; order=${rest##*:}
  U=$(post /payments "{\"amount\": $amount, \"currency\": \"EUR\", \"orderId\": \"$order\"}" | field uuid)
  post "/payments/$U/simulate" '{"outcome":"PAID","channel":"ipn"}' >/dev/null
  echo "  refus $code : $order"
done

post /payments '{"amount": 12500, "currency": "EUR", "orderId": "CMD-1044",
  "customer": {"email": "dave@example.com", "reference": "client-1044"}}' >/dev/null
echo "  paiement en attente : CMD-1044"

post /payments '{"amount": 0, "currency": "EUR", "orderId": "REGISTER-2042",
  "formAction": "REGISTER",
  "card": {"pan": "4111111111111111", "expiryMonth": 6, "expiryYear": 2028}}' >/dev/null
echo "  moyen enrole (sans contexte) : VISA"

post /payments '{"amount": 0, "currency": "EUR", "orderId": "REGISTER-2043",
  "formAction": "REGISTER",
  "card": {"pan": "4000000000000002", "expiryMonth": 1, "expiryYear": 2020,
           "holderName": "CARTE EXPIREE"}}' >/dev/null
echo "  moyen inexploitable : PAN de refus + expire"

S1=$(post /subscriptions "{\"paymentMethodToken\": \"$T1\",
  \"amount\": 2990, \"currency\": \"EUR\", \"orderId\": \"SUB-77\",
  \"effectDate\": \"2026-09-01T00:00:00Z\",
  \"rrule\": \"RRULE:FREQ=MONTHLY;INTERVAL=1\"}" | field id)
post "/subscriptions/$S1/trigger-billing" '{}' >/dev/null
echo "  abonnement + 1 echeance : SUB-77"

post /payments "{\"amount\": 1990, \"currency\": \"EUR\", \"orderId\": \"CMD-1045\",
  \"paymentMethodToken\": \"$T1\"}" >/dev/null
echo "  rejeu one-click : CMD-1045"

sleep 2

echo
echo "=== pret ==="
echo "  UI        : http://$IP:$PORT/"
echo "  image     : $IMAGE"
echo "  paiements : $(count payments uuid)   moyens : $(count payment-methods token)   abonnem. : $(count subscriptions id)   webhooks : $(count webhooks id)"
echo
echo "Bloc Client complet  : REGISTER-2041"
echo "Motifs de refus      : CMD-1043 (51), CMD-1046 (43), CMD-1047 (91)"
echo "Charges utiles       : comparer CMD-1042 et CMD-1043"
echo "Charge utile vide    : CMD-1044, aucune livraison rattachee"
echo
echo "Pour arreter : docker rm -f $NAME $SINK && docker network rm $NET"
