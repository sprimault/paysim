// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

package webui

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"sort"
	"strings"
	"sync"
)

// Version renvoie un hash court identifiant le bundle Vite embarqué.
// La valeur ne change qu'entre deux builds — un pod qui redémarre avec
// le même binaire renverra le même hash.
//
// L'API l'expose via GET /paysim/api/v1/version ; le front l'interroge
// périodiquement pour proposer un rechargement quand un nouveau bundle
// est déployé (évite le hard reload manuel).
//
// Le hash porte sur les NOMS des fichiers de `dist/assets`, pas leur
// contenu : Vite y injecte lui-même un hash de contenu dans chaque
// nom, donc un changement de bundle change forcément l'ensemble des
// noms. Suffisant, et moins coûteux qu'un hash de contenu au startup.
func Version() string {
	versionOnce.Do(computeVersion)
	return versionHash
}

var (
	versionOnce sync.Once
	versionHash string
)

func computeVersion() {
	sub, err := fs.Sub(distFS, "dist/assets")
	if err != nil {
		versionHash = "unknown"
		return
	}
	var names []string
	_ = fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		names = append(names, path)
		return nil
	})
	sort.Strings(names)
	h := sha256.New()
	h.Write([]byte(strings.Join(names, "\n")))
	versionHash = hex.EncodeToString(h.Sum(nil))[:16]
}
