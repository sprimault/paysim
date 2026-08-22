// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// Package config lit et valide la configuration du processus Paysim.
// Un seul point de lecture, au démarrage, pas de os.Getenv dispersé dans
// les autres paquets — la Config est construite ici puis injectée. Toute
// variable de secret gère sa forme _FILE pour permettre le montage de
// Secrets Kubernetes sans écrire la valeur en clair dans un manifeste.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// defaultMaxPayments est le plafond de rétention par défaut du tampon
// circulaire de paiements. Choisi assez haut pour ne pas gêner un usage
// interactif, assez bas pour qu'un pod qui tourne une semaine ne sature
// pas sa mémoire.
const defaultMaxPayments = 10000

// Config regroupe l'ensemble des paramètres validés du processus.
// Toutes les valeurs sont figées à Load ; le processus ne relit jamais
// l'environnement en cours d'exécution.
type Config struct {
	// PublicURL est l'URL vue par un navigateur : hôte d'ingress ou
	// localhost:8080. Sert aux redirections et aux liens absolus rendus
	// dans l'interface.
	PublicURL *url.URL

	// CallbackURL est l'URL utilisée par les serveurs internes (marchand,
	// job) pour appeler nos APIs — typiquement un nom de service.
	// Ne dérive jamais de PublicURL : c'est l'invariant qui casse dans
	// tout compose et tout cluster si on essaie de deviner l'une à
	// partir de l'autre.
	CallbackURL *url.URL

	// BasePath est le sous-chemin sous lequel l'ingress sert Paysim
	// (ex. "/paysim"). Vide si Paysim est servi à la racine. Toujours
	// stocké sans slash final ("/app" et non "/app/") pour que la
	// concaténation soit sans ambiguïté.
	BasePath string

	// APIToken protège l'API de contrôle. Vide = API ouverte (mode local
	// explicite, cf. CLAUDE.md). Lu depuis PAYSIM_API_TOKEN ou
	// PAYSIM_API_TOKEN_FILE (exclusifs).
	APIToken string

	// MaxPayments est le plafond de rétention en mémoire du tampon
	// circulaire de paiements. Strictement positif.
	MaxPayments int

	// LogLevel est le niveau minimum des logs émis via log/slog.
	LogLevel slog.Level

	// PayzenHMACKey est la clé HMAC-SHA-256 qui signe le retour
	// navigateur (champ kr-hash, kr-hash-key = sha256_hmac).
	// Lue depuis PAYSIM_PAYZEN_HMAC_KEY ou PAYSIM_PAYZEN_HMAC_KEY_FILE
	// (exclusifs). Vide = signature désactivée, les endpoints de
	// simulation retourneront une erreur claire au premier appel.
	PayzenHMACKey string

	// PayzenRESTPassword signe les notifications serveur à serveur
	// (kr-hash-key = password). PayZen utilise deux clés distinctes
	// selon le canal, et le SDK marchand choisit la sienne d'après
	// kr-hash-key : signer les deux avec la même laisserait la branche
	// « password » de son code jamais exercée avant la production.
	//
	// Lue depuis PAYSIM_PAYZEN_REST_PASSWORD ou son doublon _FILE.
	// Exigée dès que PayZen est utilisable, c'est-à-dire dès que la clé
	// HMAC est configurée : sans elle, l'IPN simulé ne peut pas être
	// fidèle, et un simulateur infidèle sur ce point précis ne sert à
	// rien.
	PayzenRESTPassword string

	// PayzenBrand est la marque Lyra que porte le trafic arrivant par les
	// routes du protocole. Lue depuis PAYSIM_PAYZEN_BRAND, défaut
	// "payzen".
	//
	// Elle est nécessaire parce que /api-payment/V4/* ne transporte
	// aucune marque : chez Lyra c'est l'hôte qui la désigne, et Paysim
	// n'en a qu'un. L'API de contrôle, elle, prend la marque dans le
	// corps de la requête — une instance peut donc héberger plusieurs
	// intégrations, celle du protocole recevant celle-ci par défaut.
	PayzenBrand string

	// HTTPAddr est l'adresse d'écoute du serveur HTTP, au format Go
	// (":8080", "127.0.0.1:8080"). Défaut ":8080". Un seul port pour
	// tout — interface, API de contrôle, endpoints REST V4 (invariant
	// contrat de conteneur du CLAUDE.md).
	HTTPAddr string

	// Autoplay fait jouer automatiquement l'acte de paiement à la
	// création, sans attendre un appel de simulation. Lu depuis
	// PAYSIM_AUTOPLAY. Faux par défaut.
	//
	// En production, c'est le porteur qui s'authentifie sur le
	// formulaire puis le PSP qui notifie ; rien ne se passe tant que
	// personne n'a payé. Un test de bout en bout n'a personne pour
	// jouer ce rôle, et le seul recours était d'appeler l'API de
	// simulation depuis le code marchand — ce qui fait entrer la
	// mécanique du simulateur dans le métier et valide un
	// enchaînement qui n'existera jamais en vrai.
	//
	// Activé, l'issue reste dictée par les valeurs magiques : montant
	// se terminant par 01, PAN de refus, carte expirée. Aucun levier de
	// docs/testing-cards.md ne change de comportement.
	//
	// Renonce en revanche aux issues qui supposent un porteur qui
	// n'aboutit pas — ABANDONED et EXPIRED exigent un appel de
	// simulation explicite, donc ce mode désactivé.
	Autoplay bool

	// ChaosLatencyMs est le délai (en millisecondes) ajouté par le
	// middleware chaos à chaque requête sur /api-payment/V4/*. Zéro =
	// pas de latence injectée. Invariant 5 : le chaos n'est jamais
	// actif par défaut.
	ChaosLatencyMs int

	// ChaosErrorRate est le pourcentage (0-100) de requêtes sur
	// /api-payment/V4/* qui reçoivent une 500 injectée. Zéro = pas
	// d'erreur.
	ChaosErrorRate int

	// StoreBackend choisit l'implémentation de persistance :
	//   - "memory" (défaut) : maps en mémoire, aucun état entre
	//     redémarrages ; cohérent avec un système de fichiers en
	//     lecture seule (contrat de conteneur par défaut).
	//   - "sqlite" : persistance sur disque au chemin SQLitePath.
	//     Nécessite un volume writable dans le conteneur.
	StoreBackend string

	// SQLitePath est le chemin du fichier SQLite quand StoreBackend
	// vaut "sqlite". Défaut "/data/paysim.db" — un volume monté à
	// /data côté conteneur suffit. Ignoré en backend "memory".
	SQLitePath string
}

// defaultHTTPAddr est l'adresse d'écoute par défaut si PAYSIM_HTTP_ADDR
// n'est pas fournie. Cohérent avec l'exemple documenté "localhost:8080"
// dans CLAUDE.md.
const defaultHTTPAddr = ":8080"

// defaultSQLitePath est le chemin par défaut du fichier SQLite quand
// StoreBackend=sqlite. Un rep /data est le point de montage standard
// pour un volume K8s / Docker.
const defaultSQLitePath = "/data/paysim.db"

// storeBackendMemory / storeBackendSQLite sont les seules valeurs
// acceptées pour PAYSIM_STORE.
const (
	StoreBackendMemory = "memory"
	StoreBackendSQLite = "sqlite"
)

// Load lit la configuration depuis les variables d'environnement du
// processus et retourne une struct validée. Toute erreur est unique et
// identifie la variable en cause — pas de sentinelles typées, l'appelant
// principal (main) veut juste afficher le message et sortir.
func Load() (*Config, error) {
	return loadFrom(os.LookupEnv, os.ReadFile)
}

// loadFrom est la version testable de Load : les dépendances vers
// l'environnement et le système de fichiers sont injectées. Utilisée
// directement par les tests, jamais par le reste du code applicatif.
func loadFrom(
	lookup func(string) (string, bool),
	readFile func(string) ([]byte, error),
) (*Config, error) {
	cfg := &Config{
		MaxPayments: defaultMaxPayments,
		LogLevel:    slog.LevelInfo,
		HTTPAddr:    defaultHTTPAddr,
	}

	pub, err := requiredURL(lookup, "PAYSIM_PUBLIC_URL")
	if err != nil {
		return nil, err
	}
	cfg.PublicURL = pub

	cb, err := requiredURL(lookup, "PAYSIM_CALLBACK_URL")
	if err != nil {
		return nil, err
	}
	cfg.CallbackURL = cb

	if raw, ok := lookup("PAYSIM_BASE_PATH"); ok {
		cfg.BasePath = normalizeBasePath(raw)
	}

	token, err := secretValue(lookup, readFile, "PAYSIM_API_TOKEN")
	if err != nil {
		return nil, err
	}
	cfg.APIToken = token

	hmacKey, err := secretValue(lookup, readFile, "PAYSIM_PAYZEN_HMAC_KEY")
	if err != nil {
		return nil, err
	}
	cfg.PayzenHMACKey = hmacKey

	restPassword, err := secretValue(lookup, readFile, "PAYSIM_PAYZEN_REST_PASSWORD")
	if err != nil {
		return nil, err
	}
	cfg.PayzenRESTPassword = restPassword

	// La valeur est reprise telle quelle : la liste des marques valides
	// est une connaissance du protocole, elle vit dans l'adaptateur. La
	// configuration ne doit pas importer un fournisseur — elle en
	// importerait deux le jour du deuxième. cmd/paysim valide avant de
	// câbler, et refuse de démarrer sur une marque inconnue.
	if raw, ok := lookup("PAYSIM_PAYZEN_BRAND"); ok {
		cfg.PayzenBrand = raw
	}

	// Refus au démarrage plutôt qu'au premier IPN : une instance qui
	// signe ses notifications avec la mauvaise clé valide chez le
	// marchand un chemin de vérification que la production n'emprunte
	// pas. Mieux vaut ne pas démarrer que produire cette illusion.
	if cfg.PayzenHMACKey != "" && cfg.PayzenRESTPassword == "" {
		return nil, errors.New("configuration: PAYSIM_PAYZEN_REST_PASSWORD est requis " +
			"des lors que PAYSIM_PAYZEN_HMAC_KEY est defini (signature des notifications " +
			"serveur a serveur, kr-hash-key=password)")
	}

	if raw, ok := lookup("PAYSIM_HTTP_ADDR"); ok && raw != "" {
		cfg.HTTPAddr = raw
	}

	if raw, ok := lookup("PAYSIM_CHAOS_LATENCY_MS"); ok {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("configuration: PAYSIM_CHAOS_LATENCY_MS invalide (%q)", raw)
		}
		cfg.ChaosLatencyMs = n
	}

	if raw, ok := lookup("PAYSIM_CHAOS_ERROR_RATE"); ok {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > 100 {
			return nil, fmt.Errorf("configuration: PAYSIM_CHAOS_ERROR_RATE invalide (%q), attendu 0-100", raw)
		}
		cfg.ChaosErrorRate = n
	}

	if raw, ok := lookup("PAYSIM_MAX_PAYMENTS"); ok {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("configuration: PAYSIM_MAX_PAYMENTS invalide (%q)", raw)
		}
		cfg.MaxPayments = n
	}

	if raw, ok := lookup("PAYSIM_LOG_LEVEL"); ok {
		lvl, err := parseLogLevel(raw)
		if err != nil {
			return nil, err
		}
		cfg.LogLevel = lvl
	}

	if raw, ok := lookup("PAYSIM_AUTOPLAY"); ok && raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("configuration: PAYSIM_AUTOPLAY invalide (%q), attendu un booleen", raw)
		}
		cfg.Autoplay = v
	}

	cfg.StoreBackend = StoreBackendMemory
	if raw, ok := lookup("PAYSIM_STORE"); ok && raw != "" {
		switch raw {
		case StoreBackendMemory, StoreBackendSQLite:
			cfg.StoreBackend = raw
		default:
			return nil, fmt.Errorf("configuration: PAYSIM_STORE invalide (%q), attendu %q ou %q",
				raw, StoreBackendMemory, StoreBackendSQLite)
		}
	}

	if cfg.StoreBackend == StoreBackendSQLite {
		cfg.SQLitePath = defaultSQLitePath
		if raw, ok := lookup("PAYSIM_SQLITE_PATH"); ok && raw != "" {
			cfg.SQLitePath = raw
		}
	}

	return cfg, nil
}

// requiredURL lit une URL absolue depuis l'environnement. La variable
// doit être présente et non vide : pas de défaut caché, l'invariant
// « ne jamais retomber sur localhost par défaut » est structurant — ça
// marche hors conteneur et casse dans tout compose et tout cluster.
// Seuls les schémas http et https sont acceptés.
func requiredURL(lookup func(string) (string, bool), name string) (*url.URL, error) {
	raw, ok := lookup(name)
	if !ok || raw == "" {
		return nil, fmt.Errorf("configuration: %s manquant", name)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("configuration: %s invalide (%q): %w", name, raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("configuration: %s doit avoir un schema http ou https (%q)", name, raw)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("configuration: %s doit etre absolue (%q)", name, raw)
	}
	return u, nil
}

// secretValue résout une variable de secret sous ses deux formes :
// soit la valeur directe dans NAME, soit le contenu du fichier pointé
// par NAME_FILE. Les deux définis simultanément est une erreur — la
// priorité serait arbitraire, mieux vaut échouer explicitement au
// démarrage plutôt que de charger un secret différent selon l'ordre
// de définition dans le manifeste.
func secretValue(
	lookup func(string) (string, bool),
	readFile func(string) ([]byte, error),
	name string,
) (string, error) {
	fileName := name + "_FILE"
	direct, hasDirect := lookup(name)
	path, hasFile := lookup(fileName)

	if hasDirect && hasFile {
		return "", fmt.Errorf("configuration: %s et %s tous deux definis, en choisir un", name, fileName)
	}
	if hasFile {
		if path == "" {
			return "", fmt.Errorf("configuration: %s vide", fileName)
		}
		data, err := readFile(path)
		if err != nil {
			return "", fmt.Errorf("configuration: lecture de %s (%q): %w", fileName, path, err)
		}
		// Les fichiers de Secret Kubernetes ou créés par un simple echo
		// se terminent typiquement par un saut de ligne qu'on ne veut
		// pas inclure dans la valeur du secret.
		valeur := strings.TrimRight(string(data), "\r\n \t")

		// Un fichier vide est une erreur de configuration, pas un choix.
		// Une valeur vide désactive la protection concernée — jeton
		// d'API ou signature — et l'instance démarrait alors sans
		// broncher, readyz au vert, la surface ouverte à qui l'atteint.
		// Le cas n'est pas théorique : une clé de Secret renommée, ou un
		// --from-file sur un fichier vide, suffisent.
		//
		// Ne concerne que le mode fichier. Laisser la variable directe à
		// vide reste le mode ouvert assumé du développement local.
		if valeur == "" {
			return "", fmt.Errorf("configuration: %s (%q) designe un fichier vide", fileName, path)
		}
		return valeur, nil
	}
	return direct, nil
}

// normalizeBasePath renvoie une forme canonique du BasePath : chaîne
// vide pour la racine, sinon un chemin sans slash final. Absorbe les
// variantes courantes ("app", "/app/", "/") qu'on retrouve dans les
// annotations d'ingress selon les habitudes de chaque équipe.
func normalizeBasePath(s string) string {
	s = strings.Trim(s, "/")
	if s == "" {
		return ""
	}
	return "/" + s
}

// parseLogLevel accepte les noms usuels des niveaux slog, insensibles
// à la casse et aux espaces. "warning" est accepté en synonyme de "warn"
// parce que c'est ce qu'on trouve dans une majorité de fichiers de
// configuration existants.
func parseLogLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("configuration: PAYSIM_LOG_LEVEL invalide (%q)", s)
}
