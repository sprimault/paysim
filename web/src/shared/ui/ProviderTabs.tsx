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

interface ProviderTabsProps<T> {
  value: string;
  onChange: (provider: string) => void;

  /**
   * La collection complète, et de quoi lire la marque d'une entrée.
   * Le compte est fait ici plutôt que par chaque écran : trois
   * réductions identiques finiraient par diverger, et c'est le même
   * composant qui les afficherait.
   *
   * Volontairement la collection ENTIÈRE, avant recherche et filtres
   * d'état : un onglet annonce ce que la marque contient, pas ce que
   * le filtre courant en laisse voir. Le contraire ferait varier le
   * total d'un onglet au gré d'une frappe dans la recherche.
   */
  items: readonly T[];
  providerOf: (item: T) => string;
}

export function ProviderTabs<T>({ value, onChange, items, providerOf }: ProviderTabsProps<T>) {
  const t = useT();
  const comptes = items.reduce<Record<string, number>>((acc, item) => {
    const prov = providerOf(item);
    acc[prov] = (acc[prov] ?? 0) + 1;
    return acc;
  }, {});
  return (
    <div
      className="mb-4 flex gap-1 border-b border-zinc-200 dark:border-zinc-800"
      role="tablist"
    >
      <Tab
        label={t('providerTabs.all')}
        count={items.length}
        active={value === ''}
        onClick={() => onChange('')}
      />
      {KNOWN_PROVIDERS.map((prov) => (
        <Tab
          key={prov}
          label={LIBELLES[prov] ?? prov}
          count={comptes[prov] ?? 0}
          active={value === prov}
          onClick={() => onChange(prov)}
        />
      ))}
    </div>
  );
}

function Tab({
  label,
  count,
  active,
  onClick,
}: {
  label: string;
  count: number;
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
      {/* Le zéro s'affiche, contrairement aux pastilles de navigation
          qui le masquent. Un onglet de marque vide est une information —
          c'est même celle qu'on cherche en balayant la barre — alors
          qu'une entrée de menu à zéro ne dit rien que l'écran lui-même
          ne dira mieux. */}
      <span
        className={
          'ml-1.5 rounded-full px-1.5 text-xs font-medium tabular-nums ' +
          (active
            ? 'bg-brand-100 text-brand-800 dark:bg-brand-900/50 dark:text-brand-200'
            : 'bg-zinc-100 text-zinc-500 dark:bg-zinc-800 dark:text-zinc-400')
        }
      >
        {count}
      </span>
    </button>
  );
}
