// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sprimault/paysim/internal/bus"
)

// TestSSERendLaMainSurArret vérifie qu'un flux SSE se termine quand le
// canal d'arrêt se ferme, sans que le client ait rien fait.
//
// C'est le comportement qui manquait : http.Server.Shutdown ferme les
// connexions inactives et attend les handlers actifs sans annuler leur
// contexte de requête. Un onglet de l'interface laissé ouvert retenait
// donc Shutdown pendant tout son délai, et le processus finissait en
// SIGKILL avant d'avoir fermé sa base.
func TestSSERendLaMainSurArret(t *testing.T) {
	t.Parallel()

	arret := make(chan struct{})
	handler := NewHandler(Deps{
		Store:     newMemStore(),
		Publisher: bus.New(),
		Logger:    discardLogger(),
		Arret:     arret,
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	// Délai large et jamais atteint en cas de succès : ce qui doit
	// terminer le flux, c'est l'arrêt du serveur, pas l'abandon du client.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		server.URL+"/paysim/api/v1/events/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	lu := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(resp.Body)
		lu <- err
	}()

	// Laisser le handler atteindre sa boucle de sélection avant de
	// fermer : sinon le test passerait pour une mauvaise raison.
	time.Sleep(150 * time.Millisecond)
	close(arret)

	select {
	case <-lu:
	case <-time.After(3 * time.Second):
		t.Fatal("le flux SSE n'a pas rendu la main après la fermeture du canal d'arrêt")
	}
}

// TestSSESansCanalArretNeFermePas garde le mode par défaut : un Deps qui
// ne fournit pas de canal laisse le champ à nil, et une lecture sur un
// canal nil bloque pour toujours. C'est ce qui rend le nouveau champ
// facultatif pour les appelants — dont tous les tests existants.
func TestSSESansCanalArretNeFermePas(t *testing.T) {
	t.Parallel()

	handler := NewHandler(Deps{
		Store:     newMemStore(),
		Publisher: bus.New(),
		Logger:    discardLogger(),
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		server.URL+"/paysim/api/v1/events/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Le flux ne doit s'interrompre que par l'expiration du contexte
	// client, pas de lui-même.
	_, err = io.ReadAll(resp.Body)
	if err == nil {
		t.Fatal("le flux s'est terminé seul alors qu'aucun canal d'arrêt n'était fourni")
	}
	if ctx.Err() == nil {
		t.Fatalf("le flux s'est interrompu avant l'expiration du contexte client : %v", err)
	}
}
