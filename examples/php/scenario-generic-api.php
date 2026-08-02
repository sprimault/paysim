<?php
// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// scenario-generic-api.php — exemple qui tape l'API générique Paysim
// `/paysim/api/v1/*` plutôt que les endpoints natifs PayZen. Rôle
// pédagogique : montrer où et comment se passe le champ `provider`.
//
// À l'inverse de scenario.php et scenario-complete.php qui utilisent
// les URLs natives PayZen (`/api-payment/V4/*`, contrat PayZen réel,
// pas de champ `provider`), ce script parle « Paysim » directement —
// c'est ce que font les scénarios YAML, l'UI, ou un script de test
// qui ne dépend pas d'un SDK PSP officiel.
//
// Deux surfaces distinctes :
//   - SDK natif (Lyra, stripe-php, ...)  → tape les URLs propres au
//     PSP, le provider est implicite dans l'URL.
//   - API générique Paysim                → tape /paysim/api/v1/*,
//     le provider passe dans le body en champ `provider`.
//
// Prérequis : Paysim lancé sur localhost:8080. Aucune Basic Auth
// sur l'API générique (Bearer optionnel si PAYSIM_API_TOKEN configuré
// côté serveur).

const PAYSIM_URL   = 'http://localhost:8080';
const IPN_URL      = 'http://localhost:9000/notification.php';
const AMOUNT_CENTS = 2990;
const CURRENCY     = 'EUR';
const ORDER_ID     = 'ORDER-GENERIC-001';

// -------- Étape 1 : create_payment via l'API générique -----------------

// Champ `provider` explicite. Par défaut Paysim retombe sur "payzen"
// si le champ est omis (log Debug côté serveur), mais on le nomme ici
// pour clarté et pour préparer la portabilité vers Stripe (phase 5) :
//
//   'provider' => 'stripe',   // ← à venir en phase 5, même endpoint
//
// À la différence de l'endpoint natif PayZen où l'URL fait le
// discriminant, ici l'URL est unique quel que soit le provider.
$createBody = json_encode([
    'provider'        => 'payzen',
    'amount'          => AMOUNT_CENTS,
    'currency'        => CURRENCY,
    'orderId'         => ORDER_ID,
    'formAction'      => 'REGISTER_PAY',        // pour enrôler la CB
    'notificationUrl' => IPN_URL,
    'card' => [
        'pan'         => '4111111111111111',
        'expiryMonth' => 12,
        'expiryYear'  => 2028,
    ],
]);

$create = curlJSON(PAYSIM_URL . '/paysim/api/v1/payments', 'POST', $createBody);
$createResp = json_decode($create, true);
if (empty($createResp['uuid'])) {
    fwrite(STDERR, "POST /payments a échoué : $create\n");
    exit(1);
}
$uuid  = $createResp['uuid'];
$token = $createResp['paymentMethodToken'] ?? '';
echo "✔ paiement créé — uuid=$uuid, state={$createResp['state']}, provider={$createResp['provider']}\n";
if ($token !== '') {
    echo "✔ paymentMethodToken retourné : " . substr($token, 0, 12) . "…\n";
}

// -------- Étape 2 : simulate via l'API générique -----------------------

// Endpoint /paysim/api/v1/payments/{uuid}/simulate — même surface
// générique, adressage par UUID. Le body reste au vocabulaire PayZen
// (outcome PAID/AUTHORISED/UNPAID/EXPIRED/ABANDONED) car c'est le
// vocabulaire simulate côté serveur ; côté scénario YAML, le runner
// mappe depuis le vocabulaire domain (captured/declined/...).
$simBody = json_encode([
    'outcome'         => 'PAID',
    'channel'         => 'ipn',
    'notificationUrl' => IPN_URL,
]);
$sim = curlJSON(PAYSIM_URL . '/paysim/api/v1/payments/' . $uuid . '/simulate', 'POST', $simBody);
echo "✔ simulate déclenché ($sim)\n";

// -------- Étape 3 : rejeu one-click via paymentMethodToken -------------

// Même endpoint qu'à l'étape 1, mais avec `paymentMethodToken` au lieu
// de `card`. Paysim reconnaît le mode rejeu, applique directement
// l'outcome (captured ou declined selon révocation/expiration/magic
// PAN/magic amount), retourne l'état final synchrone.
if ($token === '') {
    echo "ℹ pas de token retourné → étape rejeu ignorée\n";
    echo "\nParcours API générique OK (sans rejeu).\n";
    exit(0);
}

$replayBody = json_encode([
    'provider'           => 'payzen',
    'amount'             => AMOUNT_CENTS,
    'currency'           => CURRENCY,
    'orderId'            => ORDER_ID . '-M2',
    'paymentMethodToken' => $token,
    'notificationUrl'    => IPN_URL,
]);
$replay = curlJSON(PAYSIM_URL . '/paysim/api/v1/payments', 'POST', $replayBody);
$replayResp = json_decode($replay, true);
if (empty($replayResp['uuid'])) {
    fwrite(STDERR, "rejeu one-click a échoué : $replay\n");
    exit(1);
}
echo "✔ rejeu one-click OK — uuid={$replayResp['uuid']}, state={$replayResp['state']}\n";
echo "\nParcours API générique complet.\n";

// ---------------------------------------------------------------------

function curlJSON(string $url, string $method, string $body): string {
    $ch = curl_init($url);
    curl_setopt_array($ch, [
        CURLOPT_CUSTOMREQUEST  => $method,
        CURLOPT_POSTFIELDS     => $body,
        CURLOPT_HTTPHEADER     => ['Content-Type: application/json'],
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_TIMEOUT        => 5,
    ]);
    // Bearer optionnel : à activer si PAYSIM_API_TOKEN est configuré.
    //   curl_setopt($ch, CURLOPT_HTTPHEADER, [
    //       'Content-Type: application/json',
    //       'Authorization: Bearer <PAYSIM_API_TOKEN>',
    //   ]);
    $out = curl_exec($ch);
    if ($out === false) {
        fwrite(STDERR, "curl error: " . curl_error($ch) . "\n");
        exit(1);
    }
    curl_close($ch);
    return $out;
}
