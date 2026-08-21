> [🇬🇧 English](README.md) · [🇫🇷 Français](README.fr.md)

# Fournisseurs

Ce que Paysim simule, la version d'API à laquelle chaque référence est
écrite, et où la simulation s'arrête.

## Simulés aujourd'hui

| Fournisseur | Passerelle | Version d'API | Surface simulée | Référence |
| --- | --- | --- | --- | --- |
| PayZen | Lyra | REST V4.0 | 5 endpoints | [`payzen.fr.md`](payzen.fr.md) |
| Systempay | Lyra | REST V4.0 | 5 endpoints | même référence |
| Sogecommerce | Lyra | REST V4.0 | 5 endpoints | même référence |
| Scellius | Lyra | REST V4.0 | 5 endpoints | même référence |
| Lyra Collect | Lyra | REST V4.0 | 5 endpoints | même référence |

Ces cinq-là sont une seule passerelle sous cinq marques : un adaptateur
et une référence les couvrent toutes — seul l'hôte diffère, et l'hôte
est ce que vous faites pointer sur Paysim.
[`lyra-family.fr.md`](lyra-family.fr.md) donne les hôtes de production
réels, les pièges à vouloir les déduire, et ce qui en est délibérément
exclu (l'Inde tourne sur une autre API, pas sur une marque de celle-ci).

## Prévus

| Fournisseur | Passerelle | État |
| --- | --- | --- |
| Stripe | Stripe | Prochain — REST et webhooks JSON, axe de protocole complémentaire |
| Monetico | Crédit Mutuel / CIC | Plus tard — formulaire à sceau et redirection |

Ni l'un ni l'autre n'est simulé aujourd'hui. Un appel qui les vise
n'obtient pas de réponse dégradée de Paysim : il n'en obtient aucune.

## Ce que « version d'API » désigne ici

La colonne nomme la spécification amont sur laquelle une référence a été
écrite — pour la famille Lyra, REST V4.0 telle que publiée sur
[payzen.io](https://payzen.io/fr-FR/rest/V4.0/api/) et
[docs.lyra.com](https://docs.lyra.com/fr/rest/V4.0/api/).

C'est la version à laquelle un intégrateur compare son SDK. Ce n'est pas
l'annonce que tous les endpoints de cette version existent ici.

## La couverture est un sous-ensemble, et ce sous-ensemble est écrit

Aucune référence ici n'annonce « toute l'API ». Chacune porte deux
listes explicites : les endpoints que Paysim simule, et ceux qu'il ne
simule pas — ces derniers répondent `404` ou une erreur non modélisée,
plutôt qu'un succès plausible.

Cette distinction est la raison d'être de l'outil. Un simulateur qui
répond quelque chose de raisonnable à un appel qu'il n'implémente pas
apprend à une intégration à attendre un comportement que la production
n'aura pas.

## Quand l'API amont bouge

Rien ici n'est généré depuis la spécification du PSP, et aucune
vérification automatique ne compare les deux : ces références sont
tenues à la main, contre le code, et c'est le code qui fait foi sur ce
que Paysim fait réellement.

Un écart peut donc s'ouvrir en silence le jour où un fournisseur publie
un changement. Si vous en trouvez un — un champ qui a bougé, une valeur
qui n'est plus acceptée, un endpoint apparu — c'est un défaut qui mérite
d'être signalé, avec la capture réelle qui le montre. Les vecteurs de
signature, en particulier, ne sont jamais fabriqués ici : ils viennent
de captures réelles.

## Ce que Paysim ne prétend pas être

Les réponses annoncent `applicationVersion: 6.0.0-paysim` là où la vraie
plateforme annonce la sienne. C'est délibéré. Un simulateur qui se ferait
passer pour un serveur authentique serait un piège plutôt qu'un outil, et
tout ce qui atteint ces endpoints doit pouvoir savoir qu'il parle à un
faux.
