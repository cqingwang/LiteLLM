import { createFileRoute, useNavigate } from '@tanstack/react-router'
import z from 'zod'

import { RequestLogDetailPage } from '@/features/usage-logs/components/request-log-detail-page'

const searchSchema = z.object({
  session_id: z.string().optional(),
})

export const Route = createFileRoute('/usage-logs/detail/$detailId')({
  validateSearch: searchSchema,
  component: RequestLogDetailRoute,
})

function RequestLogDetailRoute() {
  const navigate = useNavigate()
  const { detailId } = Route.useParams()
  const { session_id: sessionId } = Route.useSearch()
  return (
    <RequestLogDetailPage
      detailId={detailId}
      sessionId={sessionId}
      onBack={() =>
        void navigate({
          to: '/usage-logs/$section',
          params: { section: 'common' },
        })
      }
    />
  )
}
