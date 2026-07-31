<?php
// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// scenario.php — orchestre un parcours de paiement complet contre
// Paysim, du côté marchand.
//
// Étapes :
//   1. Créer un paiement via /api-payment/V4/Charge/CreatePayment
//      (comme le ferait un vrai back-office marchand avec PayZen).
//   2. Déclencher un retour navigateur PAID via l'API de contrôle
//      Paysim (spécificité simulateur — remplace le SmartForm JS).
//   3. Le POST signé arrive sur return.php (servi par php -S local),
//      qui vérifie kr-hash.
//   4. Afficher un résumé.
//
// Prérequis : Paysim lancé sur localhost:8080, php -S localhost:9000
// dans ce dossier.

const PAYSIM_URL     = 'http://localhost:8080';
const RETURN_URL     = 'http://localhost:9000/return.php';
const PAYSIM_USER    = 'demo';       // Basic Auth permissive côté Paysim
const PAYSIM_PASS    = 'demo';
const AMOUNT_CENTS   = 1500;         // 15,00 EUR
const CURRENCY       = 'EUR';
const ORDER_ID       = 'ORDER-DEMO-001';

// -------- Étape 1 : CreatePayment --------------------------------------

$createBody = json_encode([
    'orderId'   => ORDER_ID,
    'amount'    => AMOUNT_CENTS,
    'currency'  => CURRENCY,
    'returnUrl' => RETURN_URL,
    'customer'  => [
        'email' => 'demo@example.com',
    ],
]);

$create = curlPost(
    PAYSIM_URL . '/api-payment/V4/Charge/CreatePayment',
    $createBody,
    ['Content-Type: application/json'],
    PAYSIM_USER, PAYSIM_PASS
);

$createResp = json_decode($create, true);
if (($createResp['status'] ?? '') !== 'SUCCESS') {
    fwrite(STDERR, "CreatePayment a échoué : $create\n");
    exit(1);
}
$formToken = $createResp['answer']['formToken'] ?? '';
echo "✔ formToken obtenu : " . substr($formToken, 0, 8) . "…\n";

// -------- Étape 2 : simuler le retour navigateur -----------------------

$simBody = json_encode([
    'formToken' => $formToken,
    'outcome'   => 'PAID',
    'cardBrand' => 'VISA',
]);

$sim = curlPost(
    PAYSIM_URL . '/paysim/simulate/browserReturn',
    $simBody,
    ['Content-Type: application/json'],
    null, null
);

$simResp = json_decode($sim, true);
if (($simResp['status'] ?? '') !== 'SUCCESS') {
    fwrite(STDERR, "simulate/browserReturn a échoué : $sim\n");
    exit(1);
}
echo "✔ retour déclenché (deliveryId : " . ($simResp['deliveryId'] ?? '?') . ")\n";
echo "✔ kr-hash annoncé : " . substr($simResp['krHash'] ?? '', 0, 16) . "…\n";

// -------- Étape 3 : attendre la livraison ------------------------------

// La livraison est asynchrone via la queue Paysim. return.php écrit
// dans retours.log — on attend son apparition (max 3s).
$logFile = __DIR__ . '/retours.log';
$deadline = microtime(true) + 3.0;
$initialSize = file_exists($logFile) ? filesize($logFile) : 0;
while (microtime(true) < $deadline) {
    clearstatcache(true, $logFile);
    if (file_exists($logFile) && filesize($logFile) > $initialSize) {
        break;
    }
    usleep(50_000); // 50ms
}

if (!file_exists($logFile) || filesize($logFile) <= $initialSize) {
    fwrite(STDERR, "✘ aucun retour reçu sur return.php après 3s — vérifier que php -S tourne\n");
    exit(2);
}

// Lire la dernière ligne du log (le retour qu'on vient d'attendre).
$last = trim(shell_exec('tail -1 ' . escapeshellarg($logFile)) ?? '');
echo "✔ retour reçu et journalisé :\n  $last\n";
echo "\nParcours complet réussi.\n";

// ---------------------------------------------------------------------

function curlPost(string $url, string $body, array $headers, ?string $user, ?string $pass): string {
    $ch = curl_init($url);
    curl_setopt_array($ch, [
        CURLOPT_POST           => true,
        CURLOPT_POSTFIELDS     => $body,
        CURLOPT_HTTPHEADER     => $headers,
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_TIMEOUT        => 5,
    ]);
    if ($user !== null && $pass !== null) {
        curl_setopt($ch, CURLOPT_USERPWD, $user . ':' . $pass);
        curl_setopt($ch, CURLOPT_HTTPAUTH, CURLAUTH_BASIC);
    }
    $out = curl_exec($ch);
    if ($out === false) {
        fwrite(STDERR, "curl error: " . curl_error($ch) . "\n");
        exit(1);
    }
    curl_close($ch);
    return $out;
}
