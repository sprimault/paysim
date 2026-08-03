// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

// Scripts exécutés AVANT le premier render React pour éviter le flash
// visuel entre l'état par défaut et l'état persistant de l'utilisateur.
//
// Servi tel quel (fichier de web/public/), inclus en tête de
// index.html en <script src> synchrone — s'exécute donc avant le
// bundle module (type=module = defer par défaut).
//
// Deux blocs indépendants, chacun dans son try/catch : un souci sur
// l'un (localStorage indisponible, matchMedia absent en JSDom) ne
// bloque pas l'autre.

(function () {
  // Applique la classe `dark` sur <html> selon localStorage.paysim.theme.
  try {
    var stored = localStorage.getItem('paysim.theme');
    var prefersDark =
      window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches;
    var dark = stored === 'dark' || ((stored === 'system' || !stored) && prefersDark);
    if (dark) document.documentElement.classList.add('dark');
  } catch (_) {
    /* localStorage indisponible en SSR/tests, no-op */
  }

  // Pose <html lang="..."> selon localStorage.paysim-lang ou navigator.language.
  // Fallback FR par défaut (audience initialement francophone).
  try {
    var storedLang = localStorage.getItem('paysim-lang');
    var lang =
      storedLang === 'en'
        ? 'en'
        : storedLang === 'fr'
          ? 'fr'
          : (navigator.language || '').toLowerCase().startsWith('en')
            ? 'en'
            : 'fr';
    document.documentElement.lang = lang;
  } catch (_) {
    /* no-op */
  }
})();
