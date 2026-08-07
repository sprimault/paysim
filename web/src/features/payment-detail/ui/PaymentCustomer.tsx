// Copyright 2026 Stéphane Primault <sprimault@users.noreply.github.com>
// SPDX-License-Identifier: Apache-2.0

import { Card } from '@/shared/ui/Card';
import { useT } from '@/shared/i18n/useT';
import type { Customer } from '@/shared/model';

/**
 * PaymentCustomer — le contexte marchand d'un paiement : identité du
 * client, facturation, livraison, contexte navigateur.
 *
 * Ce bloc existe parce que ces champs n'étaient visibles nulle part
 * ailleurs que dans le kr-answer brut de l'onglet « Charge utile ». Un
 * champ perdu en chemin y passait donc inaperçu — c'est ce qui est
 * arrivé deux fois, à customer.reference puis aux blocs shipping et
 * extra. Les afficher, c'est rendre la perte visible du premier coup.
 *
 * Chaque section disparaît quand elle est vide : sur un paiement sans
 * client — le cas courant en test — quatre rubriques creuses ne
 * diraient rien de plus qu'une absence de bloc.
 */
export function PaymentCustomer({
  customer,
  metadata,
}: {
  customer?: Customer;
  metadata?: Record<string, string>;
}) {
  const t = useT();

  const facturation = compacter([
    [t('payment.detail.customer.name'), nomComplet(customer?.billingDetails)],
    [t('payment.detail.customer.address'), customer?.billingDetails?.address],
    [t('payment.detail.customer.zipCity'), zipVille(customer?.billingDetails)],
    [t('payment.detail.customer.country'), customer?.billingDetails?.country],
    [t('payment.detail.customer.language'), customer?.billingDetails?.language],
  ]);

  const s = customer?.shippingDetails;
  const livraison = compacter([
    [t('payment.detail.customer.name'), nomComplet(s)],
    [t('payment.detail.customer.legalName'), s?.legalName],
    [t('payment.detail.customer.category'), s?.category],
    [t('payment.detail.customer.phone'), s?.phoneNumber],
    [t('payment.detail.customer.address'), adresseLivraison(s)],
    [t('payment.detail.customer.zipCity'), zipVille(s)],
    [t('payment.detail.customer.country'), s?.country],
    [t('payment.detail.customer.carrier'), s?.deliveryCompanyName],
    [t('payment.detail.customer.speed'), s?.shippingSpeed],
    [t('payment.detail.customer.method'), s?.shippingMethod],
  ]);

  const e = customer?.extraDetails;
  const contexte = compacter([
    [t('payment.detail.customer.ip'), e?.ipAddress],
    [t('payment.detail.customer.fingerprint'), e?.fingerPrintId],
    [t('payment.detail.customer.userAgent'), e?.browserUserAgent],
    [t('payment.detail.customer.accept'), e?.browserAccept],
  ]);

  const identite = compacter([
    [t('payment.detail.customer.email'), customer?.email],
    [t('payment.detail.customer.reference'), customer?.reference],
  ]);

  const meta = Object.entries(metadata ?? {});

  const vide =
    identite.length === 0 &&
    facturation.length === 0 &&
    livraison.length === 0 &&
    contexte.length === 0 &&
    meta.length === 0;

  if (vide) {
    return (
      <Card padded>
        <Titre>{t('payment.detail.customer.title')}</Titre>
        <p className="text-sm text-zinc-500 dark:text-zinc-400">
          {t('payment.detail.customer.empty')}
        </p>
      </Card>
    );
  }

  return (
    <Card padded>
      <Titre>{t('payment.detail.customer.title')}</Titre>
      <div className="grid gap-x-8 gap-y-5 sm:grid-cols-2 lg:grid-cols-3">
        <Section titre={t('payment.detail.customer.identity')} lignes={identite} />
        <Section titre={t('payment.detail.customer.billing')} lignes={facturation} />
        <Section titre={t('payment.detail.customer.shipping')} lignes={livraison} />
        <Section titre={t('payment.detail.customer.browser')} lignes={contexte} mono />
        <Section titre={t('payment.detail.customer.metadata')} lignes={meta} mono />
      </div>
    </Card>
  );
}

function Titre({ children }: { children: React.ReactNode }) {
  return (
    <h3 className="mb-3 text-xs font-medium uppercase tracking-wider text-zinc-500 dark:text-zinc-400">
      {children}
    </h3>
  );
}

function Section({
  titre,
  lignes,
  mono = false,
}: {
  titre: string;
  lignes: [string, string][];
  mono?: boolean;
}) {
  if (lignes.length === 0) return null;
  return (
    <div>
      <h4 className="mb-1.5 text-xs font-medium text-zinc-600 dark:text-zinc-300">{titre}</h4>
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
        {lignes.map(([label, valeur]) => (
          <div key={label} className="contents">
            <dt className="text-xs text-zinc-500 dark:text-zinc-400">{label}</dt>
            <dd
              className={
                'break-all text-zinc-900 dark:text-zinc-100 ' +
                (mono ? 'font-mono text-xs' : 'text-sm')
              }
            >
              {valeur}
            </dd>
          </div>
        ))}
      </dl>
    </div>
  );
}

/** Retire les lignes sans valeur — une étiquette seule n'apprend rien. */
function compacter(lignes: [string, string | undefined][]): [string, string][] {
  return lignes.filter((l): l is [string, string] => !!l[1]);
}

function nomComplet(d?: { firstName?: string; lastName?: string }): string | undefined {
  return [d?.firstName, d?.lastName].filter(Boolean).join(' ') || undefined;
}

function zipVille(d?: { zipCode?: string; city?: string }): string | undefined {
  return [d?.zipCode, d?.city].filter(Boolean).join(' ') || undefined;
}

/**
 * PayZen découpe l'adresse de livraison plus finement que celle de
 * facturation — numéro, complément et arrondissement séparés, parce que
 * ses règles antifraude les comparent un à un. À l'affichage, on les
 * recolle : c'est une adresse qu'on lit, pas un formulaire.
 */
function adresseLivraison(d?: {
  streetNumber?: string;
  address?: string;
  address2?: string;
  district?: string;
}): string | undefined {
  return (
    [d?.streetNumber, d?.address, d?.address2, d?.district].filter(Boolean).join(', ') ||
    undefined
  );
}
