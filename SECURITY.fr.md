> [🇬🇧 English](SECURITY.md) · [🇫🇷 Français](SECURITY.fr.md)

# Sécurité

## Signaler une faille

**N'ouvrez pas d'issue publique pour une faille de sécurité.**

Utilisez le bouton **Report a vulnerability** de l'onglet Security du
dépôt (GitHub Private Vulnerability Reporting). Le rapport reste privé
jusqu'à correction.

Aucune adresse de contact n'est publiée et il n'existe pas de canal
alternatif.

## Versions couvertes

Paysim est en préversion. Seul le dernier tag publié est corrigé ; il n'y
a pas de rétroportage sur les tags antérieurs.

## Périmètre

Paysim est un simulateur de prestataire de paiement destiné au
développement et aux tests automatisés. Il est délibérément permissif, et
c'est un choix de conception, pas un défaut :

- les routes du fournisseur exigent des identifiants Basic Auth mais **ne
  les vérifient jamais** — toute paire non vide est acceptée, un
  simulateur n'ayant aucun compte marchand contre lequel authentifier ;
- l'API de contrôle n'est protégée par un jeton Bearer que si
  `PAYSIM_API_TOKEN` est défini, ce qui n'est pas le cas par défaut. Ce
  jeton ne couvre que l'API de contrôle : les routes du fournisseur
  continuent d'accepter n'importe quelle paire Basic, donc de créer des
  paiements ;
- l'interface web n'a aucune authentification. Définir `PAYSIM_API_TOKEN`
  ne la retire pas — la page reste servie, seuls ses appels répondent
  401. Placer une authentification sur l'ingress si l'interface ne doit
  pas être joignable ;
- les clés HMAC et mots de passe REST figurant dans la documentation, les
  exemples et les scripts de démonstration sont des valeurs de
  démonstration publiques.

Ne sont donc **pas** des failles :

- l'absence d'authentification sur l'API de contrôle ou l'interface web
  dans la configuration par défaut ;
- les identifiants et clés de démonstration présents dans la
  documentation ;
- l'exposition de données d'une instance accessible depuis un réseau
  public — ce déploiement n'est pas supporté, voir l'avertissement en
  tête du README ;
- l'absence de chiffrement en transit sur une écoute en clair ;
- les numéros de carte conservés en clair. C'est documenté et
  délibéré : Paysim simule un PSP, un vrai numéro de carte n'a rien à y
  faire ;
- le déni de service ou l'épuisement de ressources. Rien n'est
  authentifié par défaut : quiconque atteint l'instance peut déjà la
  remplir ;
- les signatures invalides, réponses d'erreur et webhooks malformés que
  Paysim sait produire à la demande : ce sont des fonctionnalités. Un
  écart de fidélité au protocole du fournisseur simulé est un bogue
  fonctionnel — ouvrez une issue publique ;
- l'absence d'en-têtes de sécurité, de limitation de débit ou un
  paramétrage TLS faible — autant de sujets qui relèvent d'une forme de
  déploiement que ce projet ne supporte pas ;
- la sortie d'un scanner automatique sans reproduction fonctionnelle.

Sont en revanche **recevables** les défauts qui permettent à Paysim de
nuire au-delà de son propre périmètre :

- exécution de code arbitraire sur l'hôte ou évasion du conteneur ;
- traversée de chemin, lecture ou écriture hors des répertoires
  attendus ;
- dépendance vulnérable effectivement atteignable depuis le code de
  Paysim.

À noter que les requêtes sortantes vers les URL de rappel fournies par
l'appelant sont la fonction du produit, pas un défaut : faire partir un
webhook là où on le demande est précisément ce pour quoi Paysim existe.

## Traitement

Un rapport doit comporter une reproduction contre une configuration par
défaut : quelle version, quels endpoints, et ce qu'un attaquant obtient
que les sections ci-dessus ne lui accordent pas déjà. Sans cela, il est
clos.

Le projet est maintenu bénévolement et sans engagement de délai. Les
rapports sont traités au mieux, par ordre de gravité. Il n'y a ni
programme de récompense, ni accord de niveau de service.
