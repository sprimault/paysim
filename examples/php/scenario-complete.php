<?php
// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// scenario-complete.php — référence exhaustive de tous les champs
// acceptés par les endpoints Paysim, côté marchand PHP.
//
// À l'inverse de scenario.php (parcours minimal onboarding), ce
// fichier active volontairement tous les champs disponibles :
// customer complet, metadata, formAction REGISTER_PAY, notificationUrl,
// card (extension Paysim), simulate avec paymentMethodType/wallet/
// threeDSStatus/errorCode/chaos. Objectif : servir de check-list pour
// un intégrateur qui construit son propre client.
//
// Prérequis : Paysim lancé sur localhost:8080, php -S localhost:9000
// dans ce dossier.

const PAYSIM_URL   = 'http://localhost:8080';
const RETURN_URL   = 'http://localhost:9000/return.php';
const IPN_URL      = 'http://localhost:9000/notification.php';
const PAYSIM_USER  = 'demo';    // Basic Auth permissive côté Paysim
const PAYSIM_PASS  = 'demo';
const AMOUNT_CENTS = 2990;      // 29,90 EUR (montant d'abonnement)
const CURRENCY     = 'EUR';
const ORDER_ID     = 'ORDER-COMPLETE-001';

// -------- Étape 1 : CreatePayment avec tous les champs -----------------

$createBody = json_encode([
    'orderId'    => ORDER_ID,
    'amount'     => AMOUNT_CENTS,
    'currency'   => CURRENCY,

    // formAction : REGISTER_PAY demande l'enregistrement du moyen de
    // paiement à l'issue. Le webhook renverra un paymentMethodToken
    // réutilisable via /V4/Charge/CreatePayment avec paymentMethodToken.
    // Valeurs : PAYMENT (défaut) | REGISTER_PAY | ASK_REGISTER_PAY | REGISTER.
    'formAction' => 'REGISTER_PAY',

    // Customer complet — email + billingDetails complets. Tous les
    // champs sont optionnels côté PayZen ; Paysim les stocke tels quels.
    'customer' => [
        'email' => 'jane.doe@example.com',
        'billingDetails' => [
            'title'     => 'Ms',
            'firstName' => 'Jane',
            'lastName'  => 'Doe',
            'address'   => '10 rue de la Paix',
            'city'      => 'Paris',
            'zipCode'   => '75002',
            'country'   => 'FR',
            'language'  => 'FR',
        ],
    ],

    // Metadata libre — map[string]string, propagée dans le webhook.
    // Utile pour lier le paiement à des références internes marchand.
    'metadata' => [
        'plan'    => 'pro-annual',
        'invoice' => 'INV-2026-08-1234',
        'source'  => 'checkout-web',
    ],

    // URLs de retour et notification — extensions Paysim (pas de
    // config dans le back-office simulateur). Optionnelles ici mais
    // recommandées pour tester le flow complet.
    'returnUrl'       => RETURN_URL,
    'notificationUrl' => IPN_URL,

    // Card — extension Paysim (chez PayZen réel, la CB passe par le
    // SmartForm client, jamais par l'API marchand). Fournie ici pour
    // déclencher l'enrôlement systématique côté Paysim et récupérer
    // un paymentMethodToken utilisable pour les paiements récurrents.
    // Cf. docs/testing-cards.md pour les PANs magiques de refus.
    'card' => [
        'pan'         => '4111111111111111',   // Visa test, accepté
        'expiryMonth' => 12,
        'expiryYear'  => 2028,
        'brand'       => 'VISA',               // optionnel — déduit du BIN si absent
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

// -------- Étape 2 : simulate avec tous les champs ----------------------

$simBody = json_encode([
    'formToken' => $formToken,

    // outcome : PAID | AUTHORISED | UNPAID | EXPIRED | ABANDONED.
    'outcome' => 'PAID',

    // returnUrl : surcharge celle stockée dans la Transaction. Vide =
    // utilise celle du CreatePayment.
    'returnUrl' => RETURN_URL,

    // paymentMethodType : CARDS (défaut) | IP_WIRE | ... — porté par
    // KrTransaction.paymentMethodType dans le webhook.
    'paymentMethodType' => 'CARDS',

    // cardBrand : VISA | MASTERCARD | CB | AMEX | ...
    'cardBrand' => 'VISA',

    // wallet : APPLE_PAY | GOOGLEPAY | vide.
    // Positionne KrTransactionDetails.wallet dans le webhook.
    'wallet' => '',

    // threeDSStatus : SUCCESS (défaut) | CHALLENGE | FAILURE | NOT_ENROLLED.
    // Déduit authenticationType (FRICTIONLESS/CHALLENGE) automatiquement.
    'threeDSStatus' => 'SUCCESS',

    // errorCode / errorMessage : utiles pour outcome=UNPAID uniquement.
    // Remontés dans KrTransaction.errorCode/errorMessage.
    'errorCode'    => '',
    'errorMessage' => '',

    // chaos : injection de pannes sur le webhook résultant. Chaque
    // flag est indépendant, tous inertes par défaut.
    'chaos' => [
        'duplicate'          => false, // enqueue deux fois le même webhook
        'badSignature'       => false, // kr-hash altéré, le marchand doit refuser
        'raceBeforeResponse' => false, // délai 500ms sur la réponse → webhook avant réponse
    ],

    // deliveryDelayMs : retarde l'envoi du webhook (ms). Compose avec
    // deux appels successifs pour tester le out-of-order.
    'deliveryDelayMs' => 0,
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

$last = trim(shell_exec('tail -1 ' . escapeshellarg($logFile)) ?? '');
echo "✔ retour reçu et journalisé :\n  $last\n";

// -------- Étape 4 : rejeu récurrent via le token enregistré -----------

// Le webhook du 1er paiement (REGISTER_PAY) contient paymentMethodToken
// dans transactions[0].paymentMethodToken. En pratique, le marchand le
// lit depuis notification.php et le stocke en base. Ici, pour la démo,
// on l'extrait de la dernière ligne du log qui contient tout kr-answer.
$logLastLine = trim(shell_exec('tail -1 ' . escapeshellarg($logFile)) ?? '');
$krAnswer = null;
if (preg_match('/kr-answer=([^\s&]+)/', $logLastLine, $m)) {
    $krAnswer = urldecode($m[1]);
} elseif (preg_match('/(\{.*\})/', $logLastLine, $m)) {
    $krAnswer = $m[1];
}
$paymentMethodToken = null;
if ($krAnswer !== null) {
    $decoded = json_decode($krAnswer, true);
    $paymentMethodToken = $decoded['transactions'][0]['paymentMethodToken'] ?? null;
}

if ($paymentMethodToken === null) {
    echo "ℹ paymentMethodToken introuvable dans le retour — étape rejeu ignorée\n";
    echo "\nParcours complet réussi (sans étape rejeu).\n";
    exit(0);
}

echo "✔ paymentMethodToken extrait : " . substr($paymentMethodToken, 0, 12) . "…\n";

// Rejeu one-click : POST /V4/Charge/CreatePayment avec paymentMethodToken.
// Paysim reconnaît le mode rejeu, applique directement l'outcome
// (captured ou declined selon les checks du moyen — cf. testing-cards.md).
$replayBody = json_encode([
    'orderId'            => ORDER_ID . '-M2',
    'amount'             => AMOUNT_CENTS,
    'currency'           => CURRENCY,
    'paymentMethodToken' => $paymentMethodToken,
    'notificationUrl'    => IPN_URL,
]);
$replay = curlPost(
    PAYSIM_URL . '/api-payment/V4/Charge/CreatePayment',
    $replayBody,
    ['Content-Type: application/json'],
    PAYSIM_USER, PAYSIM_PASS
);
$replayResp = json_decode($replay, true);
if (($replayResp['status'] ?? '') !== 'SUCCESS') {
    fwrite(STDERR, "rejeu one-click a échoué : $replay\n");
    exit(1);
}
echo "✔ rejeu one-click OK (formToken : " . substr($replayResp['answer']['formToken'] ?? '', 0, 8) . "…)\n";
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
