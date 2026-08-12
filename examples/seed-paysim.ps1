# examples/seed-paysim.ps1 — équivalent PowerShell du script bash du
# même nom. Peuple un Paysim déjà lancé avec le jeu de démonstration :
# moyens de paiement dans chacun de leurs états, abonnements avec et
# sans échéance, paiements couvrant les états de la machine.
#
# Le jeu produit est identique à celui de demo-ui.ps1, qui monte en plus
# le conteneur et son récepteur de webhooks. Il tient en deux parties :
# des cas remarquables, chacun porteur d'une chose à voir, puis du
# volume — trente paiements répartis sur les états. Ce volume n'est pas
# décoratif : la recherche, les filtres d'état, la pagination et
# l'en-tête collant ne se jugent pas sur huit lignes.
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

# Joue l'issue d'un paiement en attente, webhook compris.
function Invoke-Simulate {
    param([string]$Uuid, [string]$Outcome = 'PAID')
    $null = Invoke-JsonPost "/payments/$Uuid/simulate" @{
        outcome         = $Outcome
        channel         = 'ipn'
        notificationUrl = $NotifUrl
    }
}

# Renvoie N fois la derniere livraison d'un paiement.
#
# L'identifiant n'est lu qu'une fois : rejouer un rejeu produit la meme
# chose, et l'identifiant ne s'empile plus depuis qu'il repart de la
# livraison d'origine. La pause laisse la livraison se terminer — c'est
# a ce moment-la qu'elle entre dans l'historique, donc dans le compte.
function Invoke-Rejeu {
    param([string]$Uuid, [int]$Fois)
    $livraisons = @(Invoke-RestMethod -Uri "$Api/webhooks?paymentUuid=$Uuid")
    if ($livraisons.Count -eq 0) { return }
    $id = $livraisons[0].id
    1..$Fois | ForEach-Object {
        $null = Invoke-RestMethod -Method Post -Uri "$Api/webhooks/$id/replay"
        Start-Sleep -Seconds 1
    }
}

# Enrôle une carte sans rien débiter et rend l'alias créé.
function Register-Card {
    param([string]$OrderId, [hashtable]$Card)
    $r = Invoke-JsonPost '/payments' @{
        amount     = 0; currency = 'EUR'; orderId = $OrderId
        formAction = 'REGISTER'; card = $Card
    }
    return $r.paymentMethodToken
}

if ($Purge) {
    Write-Host '==> Purge des paiements existants'
    $null = Invoke-RestMethod -Method Delete -Uri "$Api/payments"
    Write-Host '  purgés'
}

Write-Host '==> 1. Enrolement portant un contexte client complet'
$reg = Invoke-JsonPost '/payments' @{
    amount     = 0; currency = 'EUR'; orderId = 'REGISTER-2041'
    formAction = 'REGISTER'
    customer   = @{
        email           = 'alice.martin@example.com'
        reference       = 'client-4821'
        billingDetails  = @{
            title = 'Mme'; language = 'fr'
            firstName = 'Alice'; lastName = 'MARTIN'
            address = '1 rue de la Paix'; zipCode = '75002'
            city = 'Paris'; country = 'FR'
        }
        shippingDetails = @{
            category = 'COMPANY'; legalName = 'ACME SARL'
            firstName = 'Bob'; lastName = 'DURAND'
            phoneNumber = '+33600000000'
            address = 'avenue des Champs'; zipCode = '75008'
            city = 'Paris'; country = 'FR'
        }
        extraDetails    = @{ ipAddress = '203.0.113.7'; fingerPrintId = 'fp-9f2c1ab4' }
    }
    metadata   = @{ plan = 'pro'; source = 'onboarding' }
    card       = @{
        pan = '5555555555554444'; expiryMonth = 12; expiryYear = 2030
        holderName = 'ALICE MARTIN'; productCategory = 'DEBIT'
    }
}
$T1 = $reg.paymentMethodToken
Write-Host "  token Mastercard = $T1"

Write-Host '==> 2. Enrolements Visa et Amex'
$T2 = Register-Card 'REGISTER-2042' @{ pan = '4111111111111111'; expiryMonth = 6; expiryYear = 2028 }
$T5 = Register-Card 'REGISTER-2045' @{ pan = '371449635398431'; expiryMonth = 3; expiryYear = 2029 }
$T6 = Register-Card 'REGISTER-2046' @{ pan = '4111111111111111'; expiryMonth = 9; expiryYear = 2031 }
Write-Host "  tokens actifs = $T2, $T5, $T6"

Write-Host '==> 3. Moyen périmé — état « Expiré » côté UI'
# Une carte ne s'enrôle jamais déjà expirée : l'autorisation serait
# refusée et aucun alias ne naîtrait. On l'enregistre saine, puis on la
# fait vieillir — c'est le cas réel, et le seul qui produise un alias
# périmé à regarder dans l'interface.
$T3 = Register-Card 'REGISTER-2043' @{ pan = '4242424242424242'; expiryMonth = 12; expiryYear = 2030 }
$null = Invoke-JsonPost "/payment-methods/$T3/expire"
Write-Host "  token = $T3 — périmé"

Write-Host '==> 4. Paiement nominal (captured)'
$p = Invoke-JsonPost '/payments' @{
    amount = 4990; currency = 'EUR'; orderId = 'CMD-1042'
    customer = @{ email = 'bob@example.com'; reference = 'client-1042' }
}
Invoke-Simulate $p.uuid
$U42 = $p.uuid
Write-Host "  $($p.uuid) — captured"

Write-Host '==> 5. Trois refus, trois motifs'
# Les centimes portent le motif : .01 donne un 51, .02 un 43, .04 un 91.
foreach ($cas in @(
        @{ amount = 1001; code = '51'; order = 'CMD-1043' },
        @{ amount = 1002; code = '43'; order = 'CMD-1046' },
        @{ amount = 1004; code = '91'; order = 'CMD-1047' })) {
    $p = Invoke-JsonPost '/payments' @{
        amount = $cas.amount; currency = 'EUR'; orderId = $cas.order
    }
    Invoke-Simulate $p.uuid
    Write-Host "  $($cas.order) — declined ($($cas.code))"
    # Dernier tour de boucle : CMD-1047, garde pour les rejeux plus bas.
    $U47 = $p.uuid
}

Write-Host '==> 6. Paiement en attente, sans issue jouée'
$null = Invoke-JsonPost '/payments' @{
    amount = 12500; currency = 'EUR'; orderId = 'CMD-1044'
    customer = @{ email = 'dave@example.com'; reference = 'client-1044' }
}
Write-Host '  CMD-1044 — initiated'

Write-Host '==> 7. Paiement autorisé (fonds réservés, non débités)'
$p = Invoke-JsonPost '/payments' @{
    amount = 7500; currency = 'EUR'; orderId = 'CMD-1048'
    customer = @{ email = 'erin@example.com'; reference = 'client-1048' }
}
Invoke-Simulate $p.uuid 'AUTHORISED'
Write-Host "  $($p.uuid) — authorized"

Write-Host '==> 8. Abonnement mensuel actif + 2 échéances jouées'
$s = Invoke-JsonPost '/subscriptions' @{
    paymentMethodToken = $T1
    amount             = 2990; currency = 'EUR'; orderId = 'SUB-77'
    effectDate         = '2026-09-01T00:00:00Z'
    rrule              = 'RRULE:FREQ=MONTHLY;INTERVAL=1'
    metadata           = @{ plan = 'pro' }
}
$null = Invoke-JsonPost "/subscriptions/$($s.id)/trigger-billing"
$null = Invoke-JsonPost "/subscriptions/$($s.id)/trigger-billing"
Write-Host "  $($s.id) — SUB-77, 2 échéances"

Write-Host '==> 9. Moyen révoqué, puis échéance refusée'
# L'échéance refusée vient d'un moyen devenu inexploitable après coup, et
# non d'une carte de refus enrôlée : celle-là ne produirait aucun alias,
# donc aucun abonnement à porter.
$T4 = Register-Card 'REGISTER-2044' @{ pan = '2223000048400011'; expiryMonth = 10; expiryYear = 2030 }
$s = Invoke-JsonPost '/subscriptions' @{
    paymentMethodToken = $T4
    amount             = 990; currency = 'EUR'; orderId = 'SUB-78'
    rrule              = 'RRULE:FREQ=MONTHLY'
}
$null = Invoke-JsonPost "/payment-methods/$T4/revoke"
$null = Invoke-JsonPost "/subscriptions/$($s.id)/trigger-billing"
Write-Host "  token $T4 révoqué, $($s.id) — SUB-78 échéance refusée"

Write-Host '==> 10. Abonnement annulé, et abonnement sans échéance'
$s = Invoke-JsonPost '/subscriptions' @{
    paymentMethodToken = $T6
    amount             = 4900; currency = 'EUR'; orderId = 'SUB-79'
    rrule              = 'RRULE:FREQ=YEARLY'
}
$null = Invoke-JsonPost "/subscriptions/$($s.id)/cancel"
$null = Invoke-JsonPost '/subscriptions' @{
    paymentMethodToken = $T2
    amount             = 1490; currency = 'EUR'; orderId = 'SUB-80'
    rrule              = 'RRULE:FREQ=WEEKLY'
}
Write-Host '  SUB-79 annulé, SUB-80 sans échéance'

Write-Host '==> 11. Rejeu one-click sur l''alias enrôlé'
$null = Invoke-JsonPost '/payments' @{
    amount = 1990; currency = 'EUR'; orderId = 'CMD-1045'
    paymentMethodToken = $T1
}
Write-Host '  CMD-1045'

Write-Host '==> 12. Volume : 30 paiements répartis sur les états'
# Répartis par le rang et non tirés au hasard : deux exécutions donnent
# le même écran, ce qui rend une capture ou une comparaison reproductible.
foreach ($i in 1..30) {
    # $socle et non $base : PowerShell ignore la casse des variables, et
    # $Base porte deja l'URL de l'instance.
    $socle = (12 + $i * 7) * 100
    switch ($i % 5) {
        0 { $amount = $socle + @(1, 2, 4)[[int](($i / 5) % 3)]; $issue = 'PAID' }
        1 { $amount = $socle; $issue = $null }
        3 { $amount = $socle; $issue = 'AUTHORISED' }
        default { $amount = $socle + 50; $issue = 'PAID' }
    }
    $p = Invoke-JsonPost '/payments' @{
        amount = $amount; currency = 'EUR'; orderId = ('CMD-2{0:d3}' -f $i)
        customer = @{ email = "client$i@example.com"; reference = "client-2$i" }
    }
    if ($issue) { Invoke-Simulate $p.uuid $issue }
    if ($i -eq 12) { $U12 = $p.uuid }
}
Write-Host '  CMD-2001 à CMD-2030'

Write-Host '==> 13. Rejeux, pour que la pastille du bouton de renvoi compte'
# Des nombres differents sur trois paiements : sans rejeu, la pastille
# ne s'affiche nulle part et l'ecran ne montre pas ce qu'il sait faire.
Invoke-Rejeu $U42 1
Invoke-Rejeu $U47 2
Invoke-Rejeu $U12 3
Write-Host '  CMD-1042 (1), CMD-1047 (2), CMD-2012 (3)'

Write-Host ''
Write-Host '==> Résumé'
$payments = Invoke-RestMethod -Method Get -Uri "$Api/payments"
$subs = Invoke-RestMethod -Method Get -Uri "$Api/subscriptions"
$methods = Invoke-RestMethod -Method Get -Uri "$Api/payment-methods"
Write-Host "Paiements: $(@($payments).Count)"
Write-Host "Subscriptions: $(@($subs).Count)"
Write-Host "Payment methods: $(@($methods).Count)"
Write-Host ''
Write-Host 'Recherche : taper « client-2 » pour filtrer le volume'
Write-Host 'Pastille de rejeux : CMD-1042 (1), CMD-1047 (2), CMD-2012 (3)'
Write-Host "UI : $Base/"
