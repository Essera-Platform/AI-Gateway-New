import { createFileRoute } from '@tanstack/react-router'

import { OpenSourceNotices } from '@/features/essera/legal/open-source-notices'

export const Route = createFileRoute('/legal')({
  component: OpenSourceNotices,
})
