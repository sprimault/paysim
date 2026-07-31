// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// Binaire paysim-record : mode enregistrement de Paysim. Place un
// proxy transparent devant une vraie sandbox PSP (par exemple
// api.payzen.eu) et capture chaque échange dans un fichier .http
// versionnable. Sert à obtenir des vecteurs authentiques sans
// dépendre d'un contact ni d'un capture manuel.
//
// Usage :
//
//	paysim-record -upstream=https://api.payzen.eu -listen=:8080 -output=./captures
//
// L'intégration marchande pointe ensuite sur localhost:8080 au lieu
// de api.payzen.eu — chaque appel est enregistré et relayé.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sprimault/paysim/internal/recorder"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "paysim-record:", err)
		os.Exit(1)
	}
}

// run est extrait pour la testabilité — args et writers injectés.
func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("paysim-record", flag.ContinueOnError)
	fs.SetOutput(stderr)
	upstream := fs.String("upstream", "", "URL de la vraie sandbox PSP (ex: https://api.payzen.eu)")
	listen := fs.String("listen", ":8080", "adresse d'écoute HTTP")
	output := fs.String("output", "./captures", "dossier de sortie des captures .http")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *upstream == "" {
		fs.Usage()
		return errors.New("-upstream est obligatoire")
	}

	logger := slog.New(slog.NewJSONHandler(stdout, nil))
	rec, err := recorder.New(*upstream, *output, logger)
	if err != nil {
		return err
	}

	logger.Info("paysim_record_start",
		"upstream", *upstream,
		"listen", *listen,
		"output", *output,
	)

	server := &http.Server{
		Addr:              *listen,
		Handler:           rec.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() { serverErr <- server.ListenAndServe() }()

	select {
	case <-ctx.Done():
		logger.Info("shutdown_signal_received")
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	logger.Info("paysim_record_stopped")
	return nil
}
