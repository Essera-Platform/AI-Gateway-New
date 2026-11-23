import { useTranslation } from 'react-i18next'

import { PublicLayout } from '@/components/layout'
import { Footer } from '@/components/layout/components/footer'

export const NEW_API_ATTRIBUTION =
  'Frontend design and development by New API contributors.'
export const NEW_API_REPOSITORY_URL = 'https://github.com/QuantumNous/new-api'
const AGPL_TEXT_URL = 'https://www.gnu.org/licenses/agpl-3.0.html'

const EXTERNAL_LINK =
  'font-medium text-emerald-700 underline decoration-emerald-700/30 underline-offset-4 transition-colors hover:decoration-emerald-700 dark:text-emerald-300 dark:decoration-emerald-300/30 dark:hover:decoration-emerald-300'

const FONTS = ['JetBrains Mono', 'Lora', 'Plus Jakarta Sans', 'Public Sans']

const APACHE_NOTICES = [
  {
    name: 'AWS SDK for Go',
    lines: [
      'Copyright 2015 Amazon.com, Inc. or its affiliates. All Rights Reserved.',
      'Copyright 2014-2015 Stripe, Inc.',
    ],
  },
  {
    name: 'smithy-go',
    lines: [
      'Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.',
    ],
  },
  {
    name: 'otp',
    lines: [
      'Copyright (c) 2014, Paul Querna',
      'This product includes software developed by Paul Querna (http://paul.querna.org/).',
    ],
  },
]

const DEPENDENCY_LICENSES =
  'MIT, Apache-2.0, BSD-2-Clause, BSD-3-Clause, ISC, MPL-2.0, OFL-1.1 and Unlicense'

function Section(props: { title: string; children: React.ReactNode }) {
  return (
    <section className='bg-card space-y-3 rounded-2xl border p-6 shadow-sm sm:p-8'>
      <h2 className='text-foreground text-lg font-semibold tracking-tight'>
        {props.title}
      </h2>
      <div className='text-muted-foreground space-y-3 text-sm leading-relaxed'>
        {props.children}
      </div>
    </section>
  )
}

function ExternalLink(props: { href: string; children: React.ReactNode }) {
  return (
    <a
      href={props.href}
      target='_blank'
      rel='noopener noreferrer'
      className={EXTERNAL_LINK}
    >
      {props.children}
    </a>
  )
}

/** The Appropriate Legal Notices that NOTICE (AGPLv3 §7(b), §7(c)) asks the interface to show. */
export function OpenSourceNotices() {
  const { t, i18n } = useTranslation()
  const showGloss = (i18n.resolvedLanguage ?? i18n.language) !== 'en'
  return (
    <PublicLayout showMainContainer={false}>
      <main className='mx-auto max-w-3xl px-4 pt-32 pb-20 sm:px-6 sm:pt-36'>
        <span className='rounded-full border border-emerald-200 bg-emerald-50 px-3 py-1 text-xs font-bold tracking-widest text-emerald-700 uppercase dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-300'>
          {t('Legal')}
        </span>
        <h1 className='text-foreground mt-4 mb-10 text-3xl font-extrabold tracking-tight sm:text-4xl'>
          {t('Open-source notices')}
        </h1>
        <div className='space-y-6'>
          <Section title={t('License')}>
            <p>
              {t(
                'This service includes software licensed under the GNU Affero General Public License v3.0 (AGPL-3.0).'
              )}
            </p>
            <p>
              <ExternalLink href={AGPL_TEXT_URL}>
                {t('Read the full AGPL-3.0 text')}
              </ExternalLink>
            </p>
          </Section>

          <Section title={t('Attribution')}>
            <p className='text-foreground'>{NEW_API_ATTRIBUTION}</p>
            {/* The sentence above stays in English (AGPLv3 §7(b)); a translation only glosses it. */}
            {showGloss && <p>{t('essera.legal.attributionGloss')}</p>}
            <p>
              {t('Original project')}:{' '}
              <ExternalLink href={NEW_API_REPOSITORY_URL}>
                {NEW_API_REPOSITORY_URL}
              </ExternalLink>
            </p>
            <p>
              {t(
                'This version has been modified by Essera. Its design, frontend and integrations have been substantially rewritten.'
              )}
            </p>
          </Section>

          <Section title={t('Third-party software')}>
            <p>
              {t(
                'This service also includes third-party open-source software under the {{licenses}} licenses. Their copyright notices and license terms are kept with every distribution of this software (NOTICE and THIRD-PARTY-LICENSES.md).',
                { licenses: DEPENDENCY_LICENSES }
              )}
            </p>
            <p>
              {t('Fonts, under the SIL Open Font License 1.1:')}{' '}
              {FONTS.join(', ')}.
            </p>
            <ul className='space-y-3'>
              {APACHE_NOTICES.map((notice) => (
                <li key={notice.name}>
                  <span className='text-foreground font-medium'>
                    {notice.name}
                  </span>{' '}
                  (Apache-2.0)
                  {notice.lines.map((line) => (
                    <span key={line} className='block'>
                      {line}
                    </span>
                  ))}
                </li>
              ))}
            </ul>
          </Section>
        </div>
      </main>
      <Footer />
    </PublicLayout>
  )
}
