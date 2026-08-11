// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

/**
 * Copie dans le presse-papier, avec le repli qu'impose le contexte non
 * sécurisé.
 *
 * `navigator.clipboard` n'existe qu'en https ou sur localhost. Or
 * Paysim se sert couramment en clair sur une IP de réseau local — un
 * NodePort de cluster, un compose sur une machine de test — où l'objet
 * est tout simplement absent. Le geste le plus répété du débogage ne
 * faisait alors rien du tout, et sans le dire.
 *
 * D'où le repli par sélection : `document.execCommand('copy')` est
 * déprécié mais reste implémenté partout, et ne demande aucun contexte
 * sécurisé.
 *
 * Rend `false` quand les deux voies échouent, à charge de l'appelant de
 * le signaler. Le silence est ce qui rendait le défaut invisible.
 */
export async function copyToClipboard(texte: string): Promise<boolean> {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(texte);
      return true;
    } catch {
      // Permission refusée, ou document non focalisé : le repli
      // ci-dessous s'en sort dans les deux cas.
    }
  }
  return copieParSelection(texte);
}

/**
 * Repli historique : un champ hors écran, sélectionné, puis la commande
 * de copie du document.
 *
 * `readOnly` et non `disabled` : un champ désactivé n'est pas
 * sélectionnable. La position fixe hors cadre évite le défilement que
 * provoquerait un `focus()` sur un élément situé en bas de page.
 */
function copieParSelection(texte: string): boolean {
  const champ = document.createElement('textarea');
  champ.value = texte;
  champ.readOnly = true;
  champ.style.position = 'fixed';
  champ.style.top = '-1000px';
  champ.style.opacity = '0';
  document.body.appendChild(champ);
  try {
    champ.select();
    return document.execCommand('copy');
  } catch {
    return false;
  } finally {
    document.body.removeChild(champ);
  }
}
