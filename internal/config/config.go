// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// Package config lit et valide la configuration du processus Paysim.
// Un seul point de lecture, au démarrage, pas de os.Getenv dispersé dans
// les autres paquets — la Config est construite ici puis injectée. Toute
// variable de secret gère sa forme _FILE pour permettre le montage de
// Secrets Kubernetes sans écrire la valeur en clair dans un manifeste.
package config

import (
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
// pas sa mémoire — c'est le défaut de MailHog qu'on ne veut pas reproduire.
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

	// PayzenHMACKey est la clé HMAC-SHA-256 utilisée pour signer les
	// retours navigateur et les webhooks IPN simulés (champ kr-hash).
	// Lue depuis PAYSIM_PAYZEN_HMAC_KEY ou PAYSIM_PAYZEN_HMAC_KEY_FILE
	// (exclusifs). Vide = signature désactivée, les endpoints de
	// simulation retourneront une erreur claire au premier appel.
	PayzenHMACKey string

	// HTTPAddr est l'adresse d'écoute du serveur HTTP, au format Go
	// (":8080", "127.0.0.1:8080"). Défaut ":8080". Un seul port pour
	// tout — interface, API de contrôle, endpoints REST V4 (invariant
	// contrat de conteneur du CLAUDE.md).
	HTTPAddr string
}

// defaultHTTPAddr est l'adresse d'écoute par défaut si PAYSIM_HTTP_ADDR
// n'est pas fournie. Cohérent avec l'exemple documenté "localhost:8080"
// dans CLAUDE.md.
const defaultHTTPAddr = ":8080"

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

	if raw, ok := lookup("PAYSIM_HTTP_ADDR"); ok && raw != "" {
		cfg.HTTPAddr = raw
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
		return strings.TrimRight(string(data), "\r\n \t"), nil
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
