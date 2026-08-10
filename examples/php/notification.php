<?php
// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// notification.php — endpoint marchand qui reçoit le webhook IPN
// serveur-à-serveur simulé par Paysim. La réponse HTTP n'est jamais vue
// par un navigateur (200 vide suffit).
//
// Différence essentielle avec return.php : la clé de signature. PayZen
// signe l'IPN avec le mot de passe d'API REST et l'annonce en posant
// kr-hash-key = password, là où le retour navigateur est signé avec la
// clé HMAC de la boutique. Vérifier une notification avec la clé du
// navigateur échoue en production, même si le code « marche » face à un
// simulateur qui ne signerait qu'avec une seule clé.
//
// Écrit dans ipn.log au lieu de retours.log — permet de séparer les
// deux flux pour tester chacun indépendamment.

const REST_PASSWORD = 'mot-de-passe-rest-de-test'; // = PAYSIM_PAYZEN_REST_PASSWORD

$krAnswer     = $_POST['kr-answer']         ?? '';
$krHash       = $_POST['kr-hash']           ?? '';
$krHashAlgo   = $_POST['kr-hash-algorithm'] ?? '';
$krHashKey    = $_POST['kr-hash-key']       ?? '';

if ($krAnswer === '' || $krHash === '' || $krHashAlgo !== 'sha256_hmac') {
    logLine('KO', 'requête invalide', '');
    http_response_code(400);
    exit;
}

// Une notification signée avec autre chose que le mot de passe REST
// n'en est pas une : on refuse plutôt que de tenter l'autre clé.
if ($krHashKey !== 'password') {
    logLine('KO', 'kr-hash-key inattendu sur un IPN: ' . $krHashKey, '');
    http_response_code(400);
    exit;
}

$expected = hash_hmac('sha256', $krAnswer, REST_PASSWORD);
if (!hash_equals($expected, $krHash)) {
    logLine('KO', 'signature invalide', '');
    http_response_code(400);
    exit;
}

$answer = json_decode($krAnswer, true);
$orderStatus = $answer['orderStatus'] ?? '?';
$orderId     = $answer['orderDetails']['orderId'] ?? '?';
logLine('OK', $orderStatus, $orderId);

http_response_code(200);

function logLine(string $result, string $status, string $orderId): void {
    $line = sprintf(
        "[%s] result=%s status=%s orderId=%s\n",
        date('c'), $result, $status, $orderId
    );
    file_put_contents(__DIR__ . '/ipn.log', $line, FILE_APPEND);
}
