import { createFileRoute, useNavigate } from '@tanstack/react-router'

import { RequestLogDetailPage } from '@/features/usage-logs/components/request-log-detail-page'

export const Route = createFileRoute(
  '/_authenticated/usage-logs/detail/$detailId'
)({
  component: RequestLogDetailRoute,
})

function RequestLogDetailRoute() {
  const navigate = useNavigate()
  const { detailId } = Route.useParams()
  return (
    <RequestLogDetailPage
      detailId={detailId}
      onBack={() =>
        void navigate({
          to: '/usage-logs/$section',
          params: { section: 'common' },
        })
      }
    />
  )
}
