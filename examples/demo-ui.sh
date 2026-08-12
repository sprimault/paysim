#!/usr/bin/env bash
# examples/demo-ui.sh — monte une instance de démonstration complète et
# la peuple, pour regarder l'interface sans rien installer.
#
# Différence avec seed-paysim.sh, qui le complète plutôt qu'il ne le
# remplace : celui-ci suppose un Paysim déjà lancé et se contente de le
# peupler. Ici, tout est monté — Paysim, un récepteur de webhooks, le
# réseau qui les relie — puis détruit d'une commande. Le jeu de données
# produit est le même dans les deux, et dans leurs variantes .ps1.
#
# Ce jeu tient en deux parties. Des cas remarquables d'abord, chacun
# porteur d'une chose à voir : contexte client complet, motifs de refus
# distincts, paiement en attente, moyen expiré, moyen révoqué,
# échéance refusée, abonnement annulé. Du volume ensuite — trente
# paiements répartis sur les états. Il n'est pas décoratif : la
# recherche, les filtres d'état, la pagination et l'en-tête collant ne
# se jugent pas sur huit lignes.
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
  -e PAYSIM_PAYZEN_REST_PASSWORD=demo-rest-password \
  -e PAYSIM_LOG_LEVEL=warn \
  "$IMAGE" >/dev/null || { echo "docker run echoue"; exit 1; }

for _ in $(seq 1 30); do
  curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1 && break
  sleep 1
done

API="http://127.0.0.1:$PORT/paysim/api/v1"
post() { curl -s -X POST "$API$1" -H 'Content-Type: application/json' -d "${2:-}"; }

# Extraction par grep plutôt que par un interpréteur JSON, comme
# seed-paysim.sh : sous git-bash, `python3` n'est pas Python mais le
# raccourci du Microsoft Store, qui répond « Python est introuvable » sur
# stdout. Les champs lus sont des chaînes plates, un grep suffit — et le
# script ne dépend plus que de bash, curl et grep.
field() { grep -o "\"$1\":\"[^\"]*\"" | head -1 | sed "s/.*\":\"//; s/\"$//"; }

# Enrôle une carte sans rien débiter et rend l'alias créé.
enrole() {
  post /payments "{\"amount\":0,\"currency\":\"EUR\",\"orderId\":\"$1\",
    \"formAction\":\"REGISTER\",\"card\":$2}" | field paymentMethodToken
}

# Compte les éléments d'une collection en s'appuyant sur une clé présente
# une fois par entrée. Approximatif par nature, suffisant pour un
# affichage de fin de script.
count() { curl -s "$API/$1" | grep -o "\"$2\":" | wc -l | tr -d ' '; }

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

T2=$(enrole REGISTER-2042 '{"pan":"4111111111111111","expiryMonth":6,"expiryYear":2028}')
enrole REGISTER-2045 '{"pan":"371449635398431","expiryMonth":3,"expiryYear":2029}' >/dev/null
echo "  moyens enroles (VISA, AMEX) : actifs"

# Une carte ne s'enrole jamais deja expiree : l'autorisation serait
# refusee et aucun alias ne naitrait. On l'enregistre saine, puis on la
# fait vieillir — c'est le cas reel, et le seul qui produise un alias
# perime a regarder dans l'interface.
T3=$(enrole REGISTER-2043 '{"pan":"4242424242424242","expiryMonth":12,"expiryYear":2030}')
post "/payment-methods/$T3/expire" >/dev/null
echo "  moyen expire : $T3"

T4=$(enrole REGISTER-2044 '{"pan":"2223000048400011","expiryMonth":10,"expiryYear":2030}')
echo "  moyen a revoquer plus bas : $T4"

T6=$(enrole REGISTER-2046 '{"pan":"4111111111111111","expiryMonth":9,"expiryYear":2031}')

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
# Dernier tour de boucle : CMD-1047, gardé pour les rejeux plus bas.
U47=$U

post /payments '{"amount": 12500, "currency": "EUR", "orderId": "CMD-1044",
  "customer": {"email": "dave@example.com", "reference": "client-1044"}}' >/dev/null
echo "  paiement en attente : CMD-1044"

U8=$(post /payments '{"amount": 7500, "currency": "EUR", "orderId": "CMD-1048",
  "customer": {"email": "erin@example.com", "reference": "client-1048"}}' | field uuid)
post "/payments/$U8/simulate" '{"outcome":"AUTHORISED","channel":"ipn"}' >/dev/null
echo "  paiement autorise, non debite : CMD-1048"

S1=$(post /subscriptions "{\"paymentMethodToken\": \"$T1\",
  \"amount\": 2990, \"currency\": \"EUR\", \"orderId\": \"SUB-77\",
  \"effectDate\": \"2026-09-01T00:00:00Z\",
  \"rrule\": \"RRULE:FREQ=MONTHLY;INTERVAL=1\",
  \"metadata\": {\"plan\": \"pro\"}}" | field id)
post "/subscriptions/$S1/trigger-billing" >/dev/null
post "/subscriptions/$S1/trigger-billing" >/dev/null
echo "  abonnement + 2 echeances : SUB-77"

# L'echeance refusee vient d'un moyen devenu inexploitable apres coup, et
# non d'une carte de refus enrolee : celle-la ne produirait aucun alias,
# donc aucun abonnement a porter.
S2=$(post /subscriptions "{\"paymentMethodToken\": \"$T4\",
  \"amount\": 990, \"currency\": \"EUR\", \"orderId\": \"SUB-78\",
  \"rrule\": \"RRULE:FREQ=MONTHLY\"}" | field id)
post "/payment-methods/$T4/revoke" >/dev/null
post "/subscriptions/$S2/trigger-billing" >/dev/null
echo "  moyen revoque puis echeance refusee : SUB-78"

S3=$(post /subscriptions "{\"paymentMethodToken\": \"$T6\",
  \"amount\": 4900, \"currency\": \"EUR\", \"orderId\": \"SUB-79\",
  \"rrule\": \"RRULE:FREQ=YEARLY\"}" | field id)
post "/subscriptions/$S3/cancel" >/dev/null
echo "  abonnement annule : SUB-79"

post /subscriptions "{\"paymentMethodToken\": \"$T2\",
  \"amount\": 1490, \"currency\": \"EUR\", \"orderId\": \"SUB-80\",
  \"rrule\": \"RRULE:FREQ=WEEKLY\"}" >/dev/null
echo "  abonnement sans echeance : SUB-80"

post /payments "{\"amount\": 1990, \"currency\": \"EUR\", \"orderId\": \"CMD-1045\",
  \"paymentMethodToken\": \"$T1\"}" >/dev/null
echo "  rejeu one-click : CMD-1045"

# Volume. Les etats sont repartis par le rang et non tires au hasard :
# deux executions donnent le meme ecran, ce qui rend une capture ou une
# comparaison reproductible.
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
  U=$(post /payments "{\"amount\": $amount, \"currency\": \"EUR\", \"orderId\": \"$order\",
    \"customer\": {\"email\": \"client$i@example.com\", \"reference\": \"client-2$i\"}}" | field uuid)
  [ "$issue" = NONE ] || post "/payments/$U/simulate" "{\"outcome\":\"$issue\",\"channel\":\"ipn\"}" >/dev/null
  [ "$i" = 12 ] && U12=$U
done
echo "  volume : 30 paiements repartis sur les etats"

# Des rejeux sur trois paiements, en nombres differents : c'est la
# pastille du bouton de renvoi qui les compte, et sans eux elle ne
# s'affiche nulle part — l'ecran ne montrerait pas ce qu'il sait faire.
rejouer "$U2" 1
rejouer "$U47" 2
rejouer "$U12" 3
echo "  rejeux : CMD-1042 (1), CMD-1047 (2), CMD-2012 (3)"

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
echo "Etats des moyens     : REGISTER-2043 expire, REGISTER-2044 revoque"
echo "Abonnements          : SUB-77 (2 echeances), SUB-78 (refusee), SUB-79 (annule), SUB-80 (aucune)"
echo "Recherche            : taper « client-2 » pour filtrer le volume"
echo "Pastille de rejeux   : CMD-1042 (1), CMD-1047 (2), CMD-2012 (3)"
echo
echo "Pour arreter : docker rm -f $NAME $SINK && docker network rm $NET"
