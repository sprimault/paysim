<?php
// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// notification.php — endpoint marchand qui reçoit le webhook IPN
// serveur-à-serveur simulé par Paysim. Même mécanique de vérification
// que return.php : la seule différence côté marchand est l'URL
// d'entrée et le fait que la réponse HTTP n'est jamais vue par un
// navigateur (200 vide suffit).
//
// Écrit dans ipn.log au lieu de retours.log — permet de séparer les
// deux flux pour tester chacun indépendamment.

const HMAC_KEY = 'cle-hmac-de-test';

$krAnswer     = $_POST['kr-answer']         ?? '';
$krHash       = $_POST['kr-hash']           ?? '';
$krHashAlgo   = $_POST['kr-hash-algorithm'] ?? '';

if ($krAnswer === '' || $krHash === '' || $krHashAlgo !== 'sha256_hmac') {
    logLine('KO', 'requête invalide', '');
    http_response_code(400);
    exit;
}

$expected = hash_hmac('sha256', $krAnswer, HMAC_KEY);
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
