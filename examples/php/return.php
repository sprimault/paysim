<?php
// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// return.php — endpoint marchand qui reçoit le POST retour navigateur
// simulé par Paysim. Reproduit fidèlement la logique attendue en
// production PayZen : vérification du kr-hash avec la clé HMAC de la
// boutique, refus en cas de signature invalide.
//
// Sortie : ligne dans retours.log avec le statut (OK/KO), le status
// du paiement et l'orderId. Consommé par scenario.php pour attendre
// et afficher le résultat.

// PayZen signe avec deux clés selon le canal et annonce laquelle dans
// kr-hash-key. Les deux doivent être connues du marchand : la clé HMAC
// de boutique pour le retour navigateur, le mot de passe d'API REST
// pour la notification serveur.
const HMAC_KEY     = 'cle-hmac-de-test';          // = PAYSIM_PAYZEN_HMAC_KEY
const REST_PASSWORD = 'mot-de-passe-rest-de-test'; // = PAYSIM_PAYZEN_REST_PASSWORD

$krAnswer      = $_POST['kr-answer']      ?? '';
$krHash        = $_POST['kr-hash']        ?? '';
$krHashAlgo    = $_POST['kr-hash-algorithm'] ?? '';
$krHashKey     = $_POST['kr-hash-key']    ?? '';
$krAnswerType  = $_POST['kr-answer-type'] ?? '';

if ($krAnswer === '' || $krHash === '') {
    logLine('KO', 'champs manquants', '');
    http_response_code(400);
    exit('champs kr-answer ou kr-hash absents');
}

if ($krHashAlgo !== 'sha256_hmac') {
    logLine('KO', 'algo inattendu: ' . $krHashAlgo, '');
    http_response_code(400);
    exit('algorithme non supporté');
}

// La clé se choisit d'après kr-hash-key, jamais en dur : c'est ce que
// fait le SDK officiel, et c'est ce qui distingue un retour navigateur
// d'une notification serveur. Vérifier les deux avec la même clé
// fonctionne tant que le simulateur n'en emploie qu'une — puis échoue
// en production, où PayZen en emploie deux.
switch ($krHashKey) {
    case 'sha256_hmac':
        $key = HMAC_KEY;
        break;
    case 'password':
        $key = REST_PASSWORD;
        break;
    default:
        logLine('KO', 'kr-hash-key inattendu: ' . $krHashKey, '');
        http_response_code(400);
        exit('kr-hash-key non supporté');
}

// Vérification signature — cœur de la démonstration.
$expected = hash_hmac('sha256', $krAnswer, $key);
if (!hash_equals($expected, $krHash)) {
    logLine('KO', 'signature invalide', '');
    http_response_code(400);
    exit('signature invalide');
}

// Signature valide → lecture du payload.
$answer = json_decode($krAnswer, true);
if (!is_array($answer)) {
    logLine('KO', 'kr-answer non JSON', '');
    http_response_code(400);
    exit('kr-answer invalide');
}

$orderStatus = $answer['orderStatus'] ?? '?';
$orderId     = $answer['orderDetails']['orderId'] ?? '?';
logLine('OK', $orderStatus, $orderId);

http_response_code(200);
echo 'ok';

function logLine(string $result, string $status, string $orderId): void {
    $line = sprintf(
        "[%s] result=%s status=%s orderId=%s\n",
        date('c'), $result, $status, $orderId
    );
    file_put_contents(__DIR__ . '/retours.log', $line, FILE_APPEND);
}
