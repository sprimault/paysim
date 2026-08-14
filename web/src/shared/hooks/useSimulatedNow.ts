// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { useClockStore } from '@/shared/model/clockStore';

/**
 * useSimulatedNow retourne l'heure vue par le simulateur, et re-rend
 * l'appelant dès que l'instance est avancée ou remise à l'heure réelle.
 *
 * À utiliser partout où l'interface tranche elle-même une question de
 * date. Le cas qui l'a fait naître : l'expiration d'une carte, calculée
 * depuis l'horloge du navigateur alors que le serveur, lui, répondait
 * `usable: false` sur la même carte. Un alias affiché « Actif » que tout
 * débit refuse est exactement le mensonge silencieux que ce simulateur
 * existe pour éviter.
 *
 * Instantané pris au rendu, pas à l'appel : un badge d'état se fige
 * entre deux rendus, contrairement à un affichage relatif qui doit
 * vieillir seconde après seconde — celui-là passe par
 * useFormatRelative.
 */
export function useSimulatedNow(): Date {
  const decalageMs = useClockStore((s) => s.decalageMs);
  return new Date(Date.now() + decalageMs);
}
