// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// Package main est le point d'entrée du binaire Paysim. Il câble la
// configuration, les composants applicatifs (Store, Queue, Handler),
// le serveur HTTP et l'arrêt propre sur signal. Aucun code métier ici
// — tout est délégué aux paquets internal/.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/sprimault/paysim/internal/api"
	"github.com/sprimault/paysim/internal/bus"
	"github.com/sprimault/paysim/internal/chaos"
	"github.com/sprimault/paysim/internal/config"
	"github.com/sprimault/paysim/internal/delivery"
	"github.com/sprimault/paysim/internal/httplog"
	"github.com/sprimault/paysim/internal/providers/payzen"
	"github.com/sprimault/paysim/internal/webui"
)

// httpClientTimeout est le timeout du client HTTP qui livre les
// webhooks sortants. Suffisant pour un marchand qui répond
// normalement, court assez pour ne pas bloquer la file en cas de
// serveur pendu.
const httpClientTimeout = 10 * time.Second

// shutdownGrace est le délai laissé aux load-balancers pour router
// ailleurs après que /readyz bascule à 503, avant qu'on ferme le
// serveur. Aligné sur les habitudes Kubernetes (preStop hook).
const shutdownGrace = 2 * time.Second

// shutdownTimeout borne l'attente maximale du drain (requêtes HTTP en
// cours + queue de livraison). 30s aligné sur le terminationGracePeriod
// Kubernetes par défaut.
const shutdownTimeout = 30 * time.Second

func main() {
	if err := run(context.Background(), os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "paysim:", err)
		os.Exit(1)
	}
}

// run est le démarrage extrait pour être testable. Prend le contexte
// racine (annulable par un test) et les writers de sortie standard.
// Retourne nil sur arrêt propre, une erreur sur échec de démarrage
// ou erreur serveur non gérée.
func run(baseCtx context.Context, stdout, stderr io.Writer) error {
	cfg, err := config.Load()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return err
	}

	logger := slog.New(slog.NewJSONHandler(stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))

	logger.Info("paysim_start",
		"http_addr", cfg.HTTPAddr,
		"public_url", cfg.PublicURL.String(),
		"callback_url", cfg.CallbackURL.String(),
		"base_path", cfg.BasePath,
		"max_payments", cfg.MaxPayments,
		"hmac_configured", cfg.PayzenHMACKey != "",
		"api_token_configured", cfg.APIToken != "",
		"chaos_latency_ms", cfg.ChaosLatencyMs,
		"chaos_error_rate", cfg.ChaosErrorRate,
	)

	// Chaos reste nil (donc inerte) si la config est vide — invariant 5.
	var chaosInj *chaos.Chaos
	if cfg.ChaosLatencyMs > 0 || cfg.ChaosErrorRate > 0 {
		chaosInj = chaos.New(chaos.Config{
			LatencyMs: cfg.ChaosLatencyMs,
			ErrorRate: cfg.ChaosErrorRate,
		}, logger)
	}

	eventBus := bus.New()
	store := payzen.NewStore()
	queue := delivery.New(&http.Client{Timeout: httpClientTimeout}, logger, cfg.MaxPayments)
	queue.SetPublisher(eventBus)
	payzenHandler := payzen.NewHandler(store, queue, logger, payzen.HandlerConfig{
		HMACKey:            cfg.PayzenHMACKey,
		APIToken:           cfg.APIToken,
		Chaos:              chaosInj,
		Publisher:          eventBus,
		DefaultCallbackURL: cfg.CallbackURL.String(),
	})
	apiHandler := api.NewHandler(api.Deps{
		Store:         store,
		Queue:         queue,
		Publisher:     eventBus,
		Logger:        logger,
		Token:         cfg.APIToken,
		PayzenHandler: payzenHandler,
	})

	var ready atomic.Bool
	ready.Store(true)

	spaHandler, err := webui.Handler(cfg.BasePath)
	if err != nil {
		return fmt.Errorf("chargement du SPA embarqué: %w", err)
	}

	mux := buildMux(payzenHandler.Routes(), apiHandler, spaHandler, cfg.BasePath, &ready)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httplog.Middleware(mux, logger),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// SIGTERM sur Unix, SIGINT (Ctrl+C) partout. syscall.SIGTERM compile
	// sur Windows mais n'y est jamais émis — sans conséquence.
	ctx, stop := signal.NotifyContext(baseCtx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := queue.Run(ctx); err != nil {
			logger.Error("queue_run_error", "err", err)
		}
	}()

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http_listen", "addr", cfg.HTTPAddr)
		serverErr <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown_signal_received")
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server_error", "err", err)
			stop()
			wg.Wait()
			return err
		}
	}

	// Séquence de shutdown : readyz KO, laisser les LB router ailleurs,
	// puis fermer le serveur, puis drainer la queue. Ordre imposé par
	// l'invariant contrat de conteneur (CLAUDE.md).
	ready.Store(false)
	logger.Info("readiness_off_before_shutdown")
	time.Sleep(shutdownGrace)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("http_shutdown_error", "err", err)
	}

	stop()
	wg.Wait()

	logger.Info("paysim_stopped")
	return nil
}

// buildMux assemble le multiplexeur principal.
//
// Découpage des routes :
//   - /healthz, /readyz : à la racine, hors BasePath (kubelet probes).
//   - /paysim/api/v1/*  : à la racine, hors BasePath (dashboards stables).
//   - /api-payment/V4/* et /paysim/simulate/* : sous BasePath si non-vide,
//     à la racine sinon. Le payzenHandler porte ses propres middlewares.
//   - Tout le reste sous BasePath : le SPA — assets statiques + fallback
//     index.html pour les routes react-router (/payments/:uuid, etc.).
//
// spaHandler peut être nil dans les tests qui n'ont pas besoin du SPA.
func buildMux(payzenHandler http.Handler, apiHandler http.Handler, spaHandler http.Handler, basePath string, ready *atomic.Bool) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("shutting down"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})

	if apiHandler != nil {
		mux.Handle("/paysim/api/v1/", apiHandler)
	}

	// PayZen à ses prefixes précis : le sous-mux interne du payzenHandler
	// matche déjà /api-payment/V4/ et /paysim/simulate/ — le monter sur
	// ces mêmes prefixes délègue proprement sans capturer la racine.
	payzenPrefixes := []string{"/api-payment/V4/", "/paysim/simulate/"}
	for _, p := range payzenPrefixes {
		if basePath != "" {
			mux.Handle(basePath+p, http.StripPrefix(basePath, payzenHandler))
		} else {
			mux.Handle(p, payzenHandler)
		}
	}

	if spaHandler != nil {
		if basePath != "" {
			mux.Handle(basePath+"/", spaHandler)
		} else {
			mux.Handle("/", spaHandler)
		}
	}

	return mux
}
