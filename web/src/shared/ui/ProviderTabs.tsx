// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

/**
 * Onglets « Tous / <provider> » réutilisés par PaymentList,
 * SubscriptionList et PaymentMethodList (règle de trois validée).
 *
 * KNOWN_PROVIDERS liste les providers exposés côté UI ; ajouter
 * `stripe` ici quand la feature arrivera. Le filtrage se
 * fait côté front — le backend renvoie déjà tous les enregistrements.
 */

import { useT } from '@/shared/i18n/useT';

// Volontairement non exportée : ce fichier n'exporte qu'un composant,
// condition du Fast Refresh de Vite. Aucun autre module ne la consomme
// aujourd'hui ; si le besoin apparaît, elle ira dans shared/model.
const KNOWN_PROVIDERS: readonly string[] = [
  'payzen',
  'systempay',
  'sogecommerce',
  'scellius',
  'lyra',
];

/**
 * Nom commercial de chaque marque. La valeur technique reste celle du
 * champ `provider` — c'est elle qui filtre — mais un intégrateur
 * Sogecommerce ne se reconnaît pas dans « sogecommerce » en minuscules.
 *
 * Pas d'i18n : ce sont des noms propres, identiques dans les deux
 * langues.
 */
const LIBELLES: Record<string, string> = {
  payzen: 'PayZen',
  systempay: 'Systempay',
  sogecommerce: 'Sogecommerce',
  scellius: 'Scellius',
  lyra: 'Lyra Collect',
};

interface ProviderTabsProps {
  value: string;
  onChange: (provider: string) => void;
}

export function ProviderTabs({ value, onChange }: ProviderTabsProps) {
  const t = useT();
  return (
    <div
      className="mb-4 flex gap-1 border-b border-zinc-200 dark:border-zinc-800"
      role="tablist"
    >
      <Tab label={t('providerTabs.all')} active={value === ''} onClick={() => onChange('')} />
      {KNOWN_PROVIDERS.map((prov) => (
        <Tab
          key={prov}
          label={LIBELLES[prov] ?? prov}
          active={value === prov}
          onClick={() => onChange(prov)}
        />
      ))}
    </div>
  );
}

function Tab({
  label,
  active,
  onClick,
}: {
  label: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={
        'inline-flex items-center border-b-2 px-3 py-2 text-sm font-medium transition-colors ' +
        (active
          ? 'border-brand-600 text-brand-700 dark:border-brand-400 dark:text-brand-300'
          : 'border-transparent text-zinc-500 hover:border-zinc-300 hover:text-zinc-800 ' +
            'dark:text-zinc-400 dark:hover:border-zinc-700 dark:hover:text-zinc-200')
      }
    >
      {label}
    </button>
  );
}
