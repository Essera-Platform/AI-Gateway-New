import { createFileRoute } from '@tanstack/react-router'

import { ApiDocs } from '@/features/essera/api-docs'

export const Route = createFileRoute('/_authenticated/docs/')({
  component: ApiDocs,
})
