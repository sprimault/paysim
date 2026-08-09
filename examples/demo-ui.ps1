# examples/demo-ui.ps1 — équivalent PowerShell du script bash du même
# nom. Monte une instance de démonstration complète et la peuple, pour
# regarder l'interface sans rien installer.
#
# Différence avec seed-paysim.ps1, qui le complète plutôt qu'il ne le
# remplace : celui-ci suppose un Paysim déjà lancé et se contente de le
# peupler. Ici, tout est monté — Paysim, un récepteur de webhooks, le
# réseau qui les relie — puis détruit d'une commande.
#
# Prérequis :
#   - Docker Desktop démarré, avec le droit de créer un réseau et de
#     publier un port.
#   - PowerShell 5.1+ ou 7+ (Invoke-RestMethod est natif).
#
# Paramètres :
#   -Port        port publié sur l'hôte (défaut 30880).
#   -Image       image à monter (défaut ghcr.io/sprimault/paysim:latest).
#                Passer :edge pour l'état de la branche principale.
#   -SinkImage   image du récepteur de webhooks (défaut traefik/whoami,
#                qui répond 200 à toute requête).
#   -HostIp      adresse par laquelle le navigateur joint l'hôte
#                (défaut localhost, ce qui suffit depuis la machine même).
#
# Aucun PAYSIM_STORE n'est passé : la démo tourne sur le mode par défaut
# de l'image, sans état. C'est délibéré — un exemple qui a besoin d'une
# option pour fonctionner cache un défaut au lieu de le montrer.
#
# Non idempotent : relancer recrée tout à zéro, les conteneurs existants
# étant supprimés d'abord.

[CmdletBinding()]
param(
    [int]$Port = 30880,
    [string]$Image = 'ghcr.io/sprimault/paysim:latest',
    [string]$SinkImage = 'traefik/whoami',
    [string]$HostIp = 'localhost'
)

$ErrorActionPreference = 'Stop'

$Name = 'paysim-demo'
$Sink = 'paysim-sink'
$Net = 'paysim-demo-net'

# PowerShell transforme toute sortie sur stderr d'une commande native en
# erreur terminante quand ErrorActionPreference vaut Stop. Or `docker rm`
# sur un conteneur absent en écrit une, alors que c'est le cas nominal
# ici. Rediriger ne suffit pas : la règle s'applique avant. On abaisse
# donc la préférence le temps de l'appel, et on juge sur le code de
# sortie, qui est le signal fiable.
# Les arguments sont passés en tableau, jamais en paramètres nommés :
# PowerShell tenterait sinon de lier `-d` ou `-f` à la fonction.
function Invoke-Docker {
    param([string[]]$DockerArgs)
    $previous = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        & docker @DockerArgs 2>&1 | Out-Null
        return $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $previous
    }
}

if ((Invoke-Docker @('version')) -ne 0) {
    throw 'Docker ne repond pas. Installer Docker, ou demarrer Docker Desktop sous Windows.'
}

$null = Invoke-Docker @('rm', '-f', $Name, $Sink)
$null = Invoke-Docker @('network', 'create', $Net)

# Le récepteur tourne dans un conteneur, sur le même réseau que Paysim.
# Viser un port de l'hôte depuis un conteneur dépend de règles de
# filtrage qui varient d'une machine à l'autre ; le réseau Docker, non.
#
# Une image qui répond 200 à tout, plutôt qu'un script monté en volume :
# le montage impose un chemin de l'hôte, que git-bash réécrit et que
# Docker Desktop interprète encore autrement. Sans fichier, la question
# ne se pose plus, et les deux variantes du script restent identiques.
if ((Invoke-Docker @('run', '-d', '--name', $Sink, '--network', $Net, $SinkImage)) -ne 0) {
    throw 'recepteur de webhooks non demarre'
}

$null = Invoke-Docker @('pull', $Image)
# PUBLIC_URL est ce que voit le navigateur, CALLBACK_URL ce que vise le
# serveur pour livrer ses webhooks. Deux configurations distinctes, et
# jamais l'une déduite de l'autre : c'est ce qui casse dès qu'un ingress
# entre en jeu.
if ((Invoke-Docker @(
            'run', '-d', '--name', $Name, '--network', $Net,
            '-p', "$($Port):8080",
            '-e', "PAYSIM_PUBLIC_URL=http://$($HostIp):$Port",
            '-e', "PAYSIM_CALLBACK_URL=http://$Sink",
            '-e', 'PAYSIM_PAYZEN_HMAC_KEY=demo-hmac-key',
            '-e', 'PAYSIM_LOG_LEVEL=warn',
            $Image)) -ne 0) {
    throw 'docker run echoue'
}

$Api = "http://127.0.0.1:$Port/paysim/api/v1"
foreach ($i in 1..30) {
    try {
        $null = Invoke-RestMethod -Uri "http://127.0.0.1:$Port/healthz" -TimeoutSec 2
        break
    } catch {
        Start-Sleep -Seconds 1
    }
}

function Invoke-JsonPost {
    param([string]$Path, $Body)
    $json = if ($Body) { $Body | ConvertTo-Json -Compress -Depth 8 } else { $null }
    Invoke-RestMethod -Method Post -Uri "$Api$Path" -ContentType 'application/json' -Body $json
}

# La réponse passe par une variable avant d'être comptée : écrit en une
# seule expression, `@(Invoke-RestMethod ...).Count` encapsule le tableau
# au lieu de le dérouler et répond 1 quel que soit le nombre d'entrées.
function Get-Count {
    param([string]$Path)
    $items = Invoke-RestMethod -Uri "$Api/$Path"
    return @($items).Count
}

Write-Host '--- jeu de donnees ---'

# Enrolement portant un contexte client complet : c'est celui-ci qui
# remplit toutes les sections du bloc Client.
$reg = Invoke-JsonPost '/payments' @{
    amount     = 0
    currency   = 'EUR'
    orderId    = 'REGISTER-2041'
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
            identityCode = '12345678900011'
            firstName = 'Bob'; lastName = 'DURAND'
            phoneNumber = '+33600000000'
            streetNumber = '12'; address = 'avenue des Champs'
            address2 = 'batiment C'; district = '8e'
            zipCode = '75008'; city = 'Paris'; state = 'IDF'; country = 'FR'
            deliveryCompanyName = 'TRANSPORTEUR X'
            shippingSpeed = 'EXPRESS'; shippingMethod = 'RELAY_POINT'
        }
        extraDetails    = @{
            ipAddress = '203.0.113.7'; fingerPrintId = 'fp-9f2c1ab4'
            browserUserAgent = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)'
            browserAccept = 'text/html,application/xhtml+xml'
        }
    }
    metadata   = @{ plan = 'pro'; source = 'onboarding'; campagne = 'ete-2026' }
    card       = @{
        pan = '5555555555554444'; expiryMonth = 12; expiryYear = 2030
        holderName = 'ALICE MARTIN'; country = 'FR'
        productCategory = 'DEBIT'; issuerName = 'BANQUE DE TEST'
    }
}
$T1 = $reg.paymentMethodToken
Write-Host "  moyen enrole (contexte complet) : $T1"

$p = Invoke-JsonPost '/payments' @{
    amount = 4990; currency = 'EUR'; orderId = 'CMD-1042'
    customer = @{ email = 'bob@example.com'; reference = 'client-1042' }
}
$null = Invoke-JsonPost "/payments/$($p.uuid)/simulate" @{ outcome = 'PAID'; channel = 'ipn' }
Write-Host '  paiement capture : CMD-1042'

# Les centimes portent le motif du refus : .01 donne un 51, .02 un 43,
# .04 un 91. Trois motifs pour comparer trois charges utiles distinctes.
foreach ($cas in @(
        @{ amount = 1001; code = '51'; order = 'CMD-1043' },
        @{ amount = 1002; code = '43'; order = 'CMD-1046' },
        @{ amount = 1004; code = '91'; order = 'CMD-1047' })) {
    $r = Invoke-JsonPost '/payments' @{
        amount = $cas.amount; currency = 'EUR'; orderId = $cas.order
    }
    $null = Invoke-JsonPost "/payments/$($r.uuid)/simulate" @{ outcome = 'PAID'; channel = 'ipn' }
    Write-Host "  refus $($cas.code) : $($cas.order)"
}

$null = Invoke-JsonPost '/payments' @{
    amount = 12500; currency = 'EUR'; orderId = 'CMD-1044'
    customer = @{ email = 'dave@example.com'; reference = 'client-1044' }
}
Write-Host '  paiement en attente : CMD-1044'

$null = Invoke-JsonPost '/payments' @{
    amount = 0; currency = 'EUR'; orderId = 'REGISTER-2042'; formAction = 'REGISTER'
    card = @{ pan = '4111111111111111'; expiryMonth = 6; expiryYear = 2028 }
}
Write-Host '  moyen enrole (sans contexte) : VISA'

$null = Invoke-JsonPost '/payments' @{
    amount = 0; currency = 'EUR'; orderId = 'REGISTER-2043'; formAction = 'REGISTER'
    card = @{
        pan = '4000000000000002'; expiryMonth = 1; expiryYear = 2020
        holderName = 'CARTE EXPIREE'
    }
}
Write-Host '  moyen inexploitable : PAN de refus + expire'

$sub = Invoke-JsonPost '/subscriptions' @{
    paymentMethodToken = $T1
    amount = 2990; currency = 'EUR'; orderId = 'SUB-77'
    effectDate = '2026-09-01T00:00:00Z'
    rrule = 'RRULE:FREQ=MONTHLY;INTERVAL=1'
}
$null = Invoke-JsonPost "/subscriptions/$($sub.id)/trigger-billing" @{}
Write-Host '  abonnement + 1 echeance : SUB-77'

$null = Invoke-JsonPost '/payments' @{
    amount = 1990; currency = 'EUR'; orderId = 'CMD-1045'
    paymentMethodToken = $T1
}
Write-Host '  rejeu one-click : CMD-1045'

Start-Sleep -Seconds 2

Write-Host ''
Write-Host '=== pret ==='
Write-Host "  UI        : http://$($HostIp):$Port/"
Write-Host "  image     : $Image"
Write-Host ("  paiements : {0}   moyens : {1}   abonnem. : {2}   webhooks : {3}" -f `
    (Get-Count 'payments'), (Get-Count 'payment-methods'),
    (Get-Count 'subscriptions'), (Get-Count 'webhooks'))
Write-Host ''
Write-Host 'Bloc Client complet  : REGISTER-2041'
Write-Host 'Motifs de refus      : CMD-1043 (51), CMD-1046 (43), CMD-1047 (91)'
Write-Host 'Charges utiles       : comparer CMD-1042 et CMD-1043'
Write-Host 'Charge utile vide    : CMD-1044, aucune livraison rattachee'
Write-Host ''
Write-Host "Pour arreter : docker rm -f $Name $Sink; docker network rm $Net"
