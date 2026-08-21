> [🇬🇧 English](lyra-family.md) · [🇫🇷 Français](lyra-family.fr.md)

# La famille Lyra

**Version d'API visée** : Lyra REST V4.0, une seule référence pour les
cinq marques — [`payzen.fr.md`](payzen.fr.md). Passer par
l'[index des fournisseurs](README.fr.md) pour la table de couverture.

PayZen, Systempay, Sogecommerce, Scellius et Lyra Collect sont la même
passerelle sous cinq marques. Paysim les couvre toutes avec un seul
adaptateur, sans configuration particulière : **seul l'hôte change, et
l'hôte est ce que vous faites pointer sur Paysim.**

## Ce qui a été vérifié

Ce n'est pas une déduction de documentation. Les API de production ont
été sondées chemin par chemin, en août 2026 :

- **24 services** existants et **12 inexistants** répondent le même code
  d'erreur, chemin pour chemin, sur les quatorze hôtes de la plateforme.
  Un chemin inventé sert de témoin — il répond `INT_901` partout, ce qui
  prouve que le routeur discrimine réellement.
- **La règle de signature est unique** : HMAC-SHA-256 hexadécimal
  minuscule sur le `kr-answer` brut, `kr-hash-key` désignant la clé —
  clé HMAC au retour navigateur, mot de passe REST en notification
  serveur.
- Le préfixe `/api-payment/` est **codé en dur** dans le SDK officiel,
  non paramétrable : seul l'hôte vient de la configuration.
- Les onze archives de marque de la même version du plugin officiel
  portent le **même code de vérification**, au même condensat.

## Les hôtes réels

À passer à votre client à la place de Paysim quand vous repassez en
production. **Aucune règle ne permet de les fabriquer** — chaque marque
se déclare.

| Marque | API REST |
|---|---|
| PayZen | `api.payzen.eu` |
| Systempay | `api.systempay.fr` |
| Sogecommerce | `api-sogecommerce.societegenerale.eu` |
| Scellius | `api.scelliuspaiement.labanquepostale.fr` |
| Lyra Collect | `api.lyra.com` |

Le piège : `secure.payzen.eu` devient `paiement.systempay.fr`, le
préfixe est `api-` avec un tiret chez Sogecommerce, et le client
JavaScript est servi par un domaine dédié chez PayZen mais par l'hôte
d'API lui-même chez Sogecommerce. Un intégrateur qui dérive un hôte par
motif se trompe.

Ne pas recopier la table du fichier `.env.example` du dépôt d'exemples
officiel : au moment de cette vérification, il portait deux entrées
inexploitables — une inversion entre l'hôte d'API et l'hôte statique
pour le Brésil, et un hôte indien dont le nom ne résout plus.

## Ce qui n'en fait pas partie

**L'Inde.** `api.in.lyra.com` n'expose pas REST V4 mais une API
entièrement distincte : chemins `/pg/rest/v1/charge`, vocabulaire
d'états `DUE`, `PAID`, `DROPPED`, enveloppe d'erreur différente, webhook
déclaré à la création de la charge plutôt qu'en back-office.
L'exclusion est vérifiée dans les deux sens. C'est un autre
fournisseur, pas une marque — Paysim ne le simule pas.

**Le client JavaScript.** Paysim ne sert pas le SmartForm : votre page
le charge depuis l'hôte de votre marque. À savoir tout de même, deux
constructions coexistent — celle d'Amérique latine porte un champ que
celle d'Europe n'a pas. Sans conséquence ici, mais un intégrateur ne
peut pas supposer que le fichier servi est le même d'une marque à
l'autre.

## Ce que Paysim ne prétend pas être

L'enveloppe de réponse annonce `applicationVersion: 6.0.0-paysim`, là
où la vraie plateforme annonce sa propre version. C'est délibéré : un
simulateur qui se ferait passer pour un serveur Lyra authentique serait
un piège, pas un outil.
