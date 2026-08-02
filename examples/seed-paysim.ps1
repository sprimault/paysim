# examples/seed-paysim.ps1 — équivalent PowerShell du script bash du
# même nom. Peuple Paysim avec un jeu de démo pour voir les états
# visuels de l'UI (captured / declined / actif / révoqué / expiré).
#
# Utile en environnement Windows sans git-bash. PowerShell 5.1+ ou
# PowerShell 7+ (Invoke-RestMethod est natif, pas de dépendance).
#
# Variables d'environnement :
#   PAYSIM_URL   URL de base de Paysim (défaut http://localhost:30880).
#                Surcharger si Paysim est ailleurs.
#   NOTIF_URL    URL des webhooks (défaut http://localhost:1/discard,
#                port fermé, échec immédiat).
#
# Paramètres :
#   -Purge   Vide les paiements existants avant de peupler.
#
# Non idempotent : relancer ajoute des entrées. Utiliser -Purge pour
# repartir propre côté paiements.

[CmdletBinding()]
param([switch]$Purge)

$ErrorActionPreference = 'Stop'
$Base = if ($env:PAYSIM_URL) { $env:PAYSIM_URL } else { 'http://localhost:30880' }
$Api = "$Base/paysim/api/v1"
$NotifUrl = if ($env:NOTIF_URL) { $env:NOTIF_URL } else { 'http://localhost:1/discard' }

function Invoke-JsonPost {
    param([string]$Path, $Body)
    $json = if ($Body) { $Body | ConvertTo-Json -Compress -Depth 8 } else { $null }
    Invoke-RestMethod -Method Post -Uri "$Api$Path" -ContentType 'application/json' -Body $json
}

function Invoke-Simulate {
    param([string]$Uuid, [string]$Outcome = 'PAID')
    $null = Invoke-JsonPost "/payments/$Uuid/simulate" @{
        outcome         = $Outcome
        channel         = 'ipn'
        notificationUrl = $NotifUrl
    }
}

if ($Purge) {
    Write-Host '==> Purge des paiements existants'
    $null = Invoke-RestMethod -Method Delete -Uri "$Api/payments"
    Write-Host '  purgés'
}

Write-Host '==> 1. Paiement nominal (captured)'
$p = Invoke-JsonPost '/payments' @{ amount = 4990; currency = 'EUR'; orderId = 'ORDER-NOMINAL-001' }
Invoke-Simulate $p.uuid
Write-Host "  $($p.uuid) — captured"

Write-Host '==> 2. Paiement refus magic amount (xxx01 → UNPAID)'
$p = Invoke-JsonPost '/payments' @{ amount = 1001; currency = 'EUR'; orderId = 'ORDER-MAGIC-AMOUNT' }
Invoke-Simulate $p.uuid
Write-Host "  $($p.uuid) — declined (magic amount)"

Write-Host '==> 3. Paiement autorisé (fonds réservés, non débités)'
$p = Invoke-JsonPost '/payments' @{ amount = 7500; currency = 'EUR'; orderId = 'ORDER-AUTH-ONLY' }
Invoke-Simulate $p.uuid 'AUTHORISED'
Write-Host "  $($p.uuid) — authorized"

Write-Host '==> 4. Enrolement Visa valide (long-terme, dates 2028)'
$p = Invoke-JsonPost '/payments' @{
    amount     = 2990; currency = 'EUR'; orderId = 'SUB-INIT-VISA'
    formAction = 'REGISTER_PAY'
    card       = @{ pan = '4111111111111111'; expiryMonth = 12; expiryYear = 2028; brand = 'VISA' }
}
$tokenVisa = $p.paymentMethodToken
Invoke-Simulate $p.uuid
Write-Host "  $($p.uuid) — captured, token Visa = $tokenVisa"

Write-Host '==> 5. Subscription mensuelle active + 2 renewals réussis'
$s = Invoke-JsonPost '/subscriptions' @{
    paymentMethodToken = $tokenVisa
    amount             = 2990; currency = 'EUR'; orderId = 'SUB-42-PRO'
    effectDate         = '2026-09-01'
    rrule              = 'RRULE:FREQ=MONTHLY;INTERVAL=1'
    metadata           = @{ plan = 'pro' }
}
Write-Host "  subscription $($s.id)"
1..2 | ForEach-Object {
    $null = Invoke-RestMethod -Method Post -Uri "$Api/subscriptions/$($s.id)/trigger-billing"
    Write-Host "  renewal $_ triggered"
}

Write-Host '==> 6. Enrolement magic PAN Visa 4000...02 (refus systématique)'
$p = Invoke-JsonPost '/payments' @{
    amount     = 1500; currency = 'EUR'; orderId = 'CARD-MAGIC-DECLINE'
    formAction = 'REGISTER_PAY'
    card       = @{ pan = '4000000000000002'; expiryMonth = 12; expiryYear = 2028; brand = 'VISA' }
}
$tokenMagic = $p.paymentMethodToken
Invoke-Simulate $p.uuid
Write-Host "  $($p.uuid) — declined (magic PAN au simulate), token = $tokenMagic"

Write-Host '==> 7. Subscription sur CB magic PAN → renewal declined'
$s = Invoke-JsonPost '/subscriptions' @{
    paymentMethodToken = $tokenMagic
    amount             = 990; currency = 'EUR'; orderId = 'SUB-FAILING'
    rrule              = 'RRULE:FREQ=MONTHLY'
}
$null = Invoke-RestMethod -Method Post -Uri "$Api/subscriptions/$($s.id)/trigger-billing"
Write-Host "  subscription $($s.id) + renewal (declined)"

Write-Host '==> 8. Enrolement Mastercard valide'
$p = Invoke-JsonPost '/payments' @{
    amount     = 3500; currency = 'EUR'; orderId = 'MC-CHECKOUT'
    formAction = 'REGISTER_PAY'
    card       = @{ pan = '5555555555554444'; expiryMonth = 6; expiryYear = 2029 }
}
Invoke-Simulate $p.uuid
Write-Host "  $($p.uuid) — captured"

Write-Host '==> 9. Enrolement Amex valide'
$p = Invoke-JsonPost '/payments' @{
    amount     = 8900; currency = 'EUR'; orderId = 'AMEX-CHECKOUT'
    formAction = 'REGISTER_PAY'
    card       = @{ pan = '371449635398431'; expiryMonth = 3; expiryYear = 2027 }
}
Invoke-Simulate $p.uuid
Write-Host "  $($p.uuid) — captured"

Write-Host '==> 10. Enrolement CB expirée (01/2020) — état « Expiré » côté UI'
$p = Invoke-JsonPost '/payments' @{
    amount     = 1200; currency = 'EUR'; orderId = 'CARD-EXPIRED'
    formAction = 'REGISTER_PAY'
    card       = @{ pan = '4242424242424242'; expiryMonth = 1; expiryYear = 2020; brand = 'VISA' }
}
$tokenExp = $p.paymentMethodToken
Invoke-Simulate $p.uuid
Write-Host "  $($p.uuid) — declined (CB expirée au simulate), token = $tokenExp"

Write-Host '==> 11. Enrolement Mastercard série 2 (nouveau BIN) puis révocation'
$p = Invoke-JsonPost '/payments' @{
    amount     = 2200; currency = 'EUR'; orderId = 'MC2-CHECKOUT'
    formAction = 'REGISTER_PAY'
    card       = @{ pan = '2223000048400011'; expiryMonth = 10; expiryYear = 2030 }
}
$tokenMc2 = $p.paymentMethodToken
Invoke-Simulate $p.uuid
$null = Invoke-RestMethod -Method Post -Uri "$Api/payment-methods/$tokenMc2/revoke"
Write-Host "  $($p.uuid) — captured + moyen révoqué manuellement"

Write-Host ''
Write-Host '==> Résumé'
$payments = Invoke-RestMethod -Method Get -Uri "$Api/payments"
$subs = Invoke-RestMethod -Method Get -Uri "$Api/subscriptions"
$methods = Invoke-RestMethod -Method Get -Uri "$Api/payment-methods"
Write-Host "Paiements: $($payments.Count)"
Write-Host "Subscriptions: $($subs.Count)"
Write-Host "Payment methods: $($methods.Count)"
Write-Host ''
Write-Host "UI : $Base/"
