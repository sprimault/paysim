> [🇬🇧 English](README.md) · [🇫🇷 Français](README.fr.md)

# Exemple d'intégration PHP

Ce dossier illustre un parcours de paiement complet contre Paysim, du
côté marchand, en PHP pur (aucune dépendance Composer). Trois scripts :

- **`scenario.php`** — orchestre la simulation : appelle
  `POST /api-payment/V4/Charge/CreatePayment` pour obtenir un `formToken`,
  puis `POST /paysim/simulate/browserReturn` pour déclencher un retour
  signé vers `return.php`.
- **`return.php`** — endpoint qui reçoit le POST retour navigateur
  (`kr-answer` + `kr-hash`), **vérifie la signature avec
  `hash_hmac('sha256', ...)`** et écrit le résultat dans un log local.
- **`notification.php`** — équivalent pour l'IPN serveur-à-serveur,
  même mécanique de vérification.

Cet exemple valide le critère de fin de phase 1 : un marchand PHP
effectue un paiement complet contre Paysim sans modification autre que
l'URL de base, et vérifie côté marchand la signature `kr-hash` produite
par Paysim.

## Prérequis

- PHP 8.1+ avec les modules `curl` et `openssl` (standards).
- Le binaire Paysim compilé (`make build` à la racine).

## Mode d'emploi

Dans trois terminaux distincts :

### 1. Lancer Paysim

```bash
export PAYSIM_PUBLIC_URL="http://localhost:30880"
export PAYSIM_CALLBACK_URL="http://localhost:9000"
export PAYSIM_PAYZEN_HMAC_KEY="cle-hmac-de-test"
./paysim
```

### 2. Lancer le serveur PHP de démonstration (marchand)

Depuis ce dossier :

```bash
php -S localhost:9000
```

Il servira `return.php` et `notification.php` sur `http://localhost:9000/`.

### 3. Lancer le scénario

```bash
php scenario.php
```

Le script :

1. Appelle Paysim → obtient un `formToken`.
2. Déclenche un retour PAID via `/paysim/simulate/browserReturn`.
3. Le serveur PHP reçoit le POST sur `return.php`, vérifie `kr-hash`,
   écrit dans `retours.log`.
4. `scenario.php` affiche un résumé du parcours.

## Vérification de signature

Le cœur de la démonstration : `return.php` recalcule le hash côté
marchand avec la même clé HMAC et compare en temps constant.

```php
$expected = hash_hmac('sha256', $krAnswer, $hmacKey);
if (!hash_equals($expected, $krHash)) {
    http_response_code(400);
    exit('signature invalide');
}
```

C'est la logique exacte qu'un marchand production doit implémenter —
Paysim la reproduit fidèlement, un intégrateur peut basculer entre
Paysim (test) et PayZen (production) sans modifier ce code.

## Clé HMAC

La clé de test dans les scripts (`cle-hmac-de-test`) doit être
identique à `PAYSIM_PAYZEN_HMAC_KEY` côté serveur Paysim, sinon la
vérification `kr-hash` échoue systématiquement.
