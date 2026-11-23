import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { KeyRound } from 'lucide-react'
import { lazy, Suspense } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import { LoadingState } from '@/components/loading-state'
import { buttonVariants } from '@/components/ui/button'
import { api } from '@/lib/api'

import { documentLanguage, type DocumentLanguage } from './document-language'

const ApiReferenceViewer = lazy(() => import('./api-reference-viewer'))

export const OPENAPI_DOCUMENT_URL = '/api/essera/openapi.json'

async function getOpenApiDocument(
  language: DocumentLanguage
): Promise<Record<string, unknown>> {
  const url =
    language === 'en'
      ? OPENAPI_DOCUMENT_URL
      : `${OPENAPI_DOCUMENT_URL}?lang=${language}`
  const res = await api.get<Record<string, unknown>>(url)
  return res.data
}

/**
 * The customer API's reference, from the OpenAPI document the Gateway serves with its own
 * address and models the reader may call. The viewer is loaded only on this page.
 */
export function ApiDocs() {
  const { t, i18n } = useTranslation()
  const language = documentLanguage(i18n.resolvedLanguage ?? i18n.language)
  const document = useQuery({
    queryKey: ['essera', 'openapi-document', language],
    queryFn: () => getOpenApiDocument(language),
    staleTime: 5 * 60 * 1000,
    meta: { errorToast: false },
  })

  let content = <LoadingState message={t('Loading API documentation...')} />
  if (document.isError) {
    content = (
      <ErrorState
        title={t('The API documentation could not be loaded.')}
        onRetry={() => document.refetch()}
      />
    )
  } else if (document.data) {
    content = (
      <Suspense fallback={content}>
        <ApiReferenceViewer
          key={language}
          document={document.data}
          language={language}
        />
      </Suspense>
    )
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('API Docs')}</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t(
          'The OpenAPI 3.1 reference of the Gateway: endpoints, authentication, errors and code samples.'
        )}
      </SectionPageLayout.Description>
      <SectionPageLayout.Actions>
        <Link to='/keys' className={buttonVariants()}>
          <KeyRound aria-hidden='true' />
          {t('View API Keys')}
        </Link>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>{content}</SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
