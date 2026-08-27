import { useQuery } from '@tanstack/react-query'
import { ArrowLeft, Copy } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'

import { getLogDetail } from '../api'

function isJsonPayload(body: string, contentType: string): boolean {
  if (contentType.toLowerCase().includes('json')) return true
  if (!body) return false
  try {
    JSON.parse(body)
    return true
  } catch {
    return false
  }
}

function formatJsonPayload(body: string): string {
  try {
    return JSON.stringify(JSON.parse(body), null, 2)
  } catch {
    return body
  }
}

function renderJsonTokens(code: string): ReactNode[] {
  const tokenPattern =
    /"(?:\\.|[^"\\])*"(?=\s*:)|"(?:\\.|[^"\\])*"|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?|true|false|null|[{}[\],:]/g
  const parts: ReactNode[] = []
  let lastIndex = 0
  let match: RegExpExecArray | null

  while ((match = tokenPattern.exec(code)) !== null) {
    if (match.index > lastIndex) {
      parts.push(code.slice(lastIndex, match.index))
    }
    const token = match[0]
    const tokenClass = token.startsWith('"')
      ? tokenPattern.lastIndex < code.length &&
        /^\s*:/.test(code.slice(tokenPattern.lastIndex))
        ? 'text-sky-600 dark:text-sky-400'
        : 'text-emerald-600 dark:text-emerald-400'
      : token === 'true' || token === 'false' || token === 'null'
        ? 'text-purple-600 dark:text-purple-400'
        : /^-?\d/.test(token)
          ? 'text-amber-600 dark:text-amber-400'
          : 'text-muted-foreground'
    parts.push(
      <span key={`${match.index}-${token}`} className={tokenClass}>
        {token}
      </span>
    )
    lastIndex = tokenPattern.lastIndex
  }
  if (lastIndex < code.length) parts.push(code.slice(lastIndex))

  return parts
}

function JsonSyntaxCode({ code }: { code: string }) {
  return (
    <pre className='bg-muted/20 min-h-48 flex-1 overflow-auto p-4 font-mono text-xs leading-5 break-all whitespace-pre-wrap'>
      {renderJsonTokens(code)}
    </pre>
  )
}

function EventStreamSyntaxCode({ code }: { code: string }) {
  const lines = code.split(/(\r\n|\n|\r)/)
  return (
    <pre className='bg-muted/20 min-h-48 flex-1 overflow-auto p-4 font-mono text-xs leading-5 break-all whitespace-pre-wrap'>
      {lines.map((line, index) => {
        if (/^\r\n$|^\n$|^\r$/.test(line)) return line
        const dataMatch = /^(\s*data:\s?)(.*)$/.exec(line)
        if (!dataMatch || !isJsonPayload(dataMatch[2], 'application/json')) {
          return line
        }
        return (
          <span key={`${index}-${line}`}>
            {dataMatch[1]}
            {renderJsonTokens(dataMatch[2])}
          </span>
        )
      })}
    </pre>
  )
}

function parseJsonObject(value: string): Record<string, unknown> | null {
  if (!value) return null
  try {
    const parsed: unknown = JSON.parse(value)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return null
    }
    return parsed as Record<string, unknown>
  } catch {
    return null
  }
}

function PayloadPanel(props: {
  title: string
  body: string
  contentType: string
  truncated: boolean
}) {
  const { t } = useTranslation()
  const { copyToClipboard } = useCopyToClipboard()
  const isJson = isJsonPayload(props.body, props.contentType)
  const isEventStream = props.contentType
    .toLowerCase()
    .includes('text/event-stream')
  const displayBody = isJson ? formatJsonPayload(props.body) : props.body
  return (
    <section className='flex min-h-0 flex-1 flex-col rounded-lg border'>
      <div className='flex items-center justify-between border-b px-4 py-3'>
        <div>
          <h2 className='font-semibold'>{props.title}</h2>
          <p className='text-muted-foreground text-xs'>
            {props.contentType || t('Unknown')}
          </p>
        </div>
        <Button
          variant='ghost'
          size='sm'
          disabled={!props.body}
          onClick={() => copyToClipboard(props.body)}
          aria-label={t('Copy')}
        >
          <Copy className='size-4' />
        </Button>
      </div>
      {props.body ? (
        isEventStream ? (
          <EventStreamSyntaxCode code={props.body} />
        ) : isJson ? (
          <JsonSyntaxCode code={displayBody} />
        ) : (
          <pre className='bg-muted/20 min-h-48 flex-1 overflow-auto p-4 font-mono text-xs leading-5 break-all whitespace-pre-wrap'>
            {displayBody}
          </pre>
        )
      ) : (
        <pre className='bg-muted/20 min-h-48 flex-1 overflow-auto p-4 text-xs break-all whitespace-pre-wrap'>
          {t('No content')}
        </pre>
      )}
      {props.truncated && (
        <p className='border-t px-4 py-2 text-xs text-amber-600'>
          {t('Content was truncated')}
        </p>
      )}
    </section>
  )
}

function HeaderPreview(props: {
  title: string
  value: string
  collapsible?: boolean
}) {
  const { t } = useTranslation()
  let value = props.value
  try {
    value = JSON.stringify(JSON.parse(props.value), null, 2)
  } catch {
    // Header values are allowed to be plain text.
  }
  const content = (
    <div className='bg-muted/20 max-h-80 overflow-auto p-2'>
      {(() => {
        const headers = parseJsonObject(props.value)
        if (!headers) {
          return (
            <pre className='p-2 text-xs break-all whitespace-pre-wrap'>
              {value || t('No content')}
            </pre>
          )
        }
        return Object.entries(headers).map(([key, headerValue]) => (
          <div
            key={key}
            className='grid grid-cols-[minmax(0,0.32fr)_minmax(0,0.68fr)] items-center gap-3 border-b py-1 text-xs last:border-0'
          >
            <span className='truncate font-medium' title={key}>
              {key}
            </span>
            <span
              className='truncate font-mono text-[11px]'
              title={String(headerValue)}
            >
              {String(headerValue)}
            </span>
          </div>
        ))
      })()}
    </div>
  )

  if (!props.collapsible) {
    return (
      <section className='rounded-lg border'>
        <h2 className='border-b px-4 py-3 font-semibold'>{props.title}</h2>
        {content}
      </section>
    )
  }

  return (
    <section className='rounded-lg border'>
      <Accordion defaultValue={[]}>
        <AccordionItem value='headers' className='border-0 px-4'>
          <AccordionTrigger className='py-3 hover:no-underline'>
            {props.title}
          </AccordionTrigger>
          <AccordionContent>{content}</AccordionContent>
        </AccordionItem>
      </Accordion>
    </section>
  )
}

export function RequestLogDetailPage(props: {
  detailId: string
  onBack: () => void
}) {
  const { t } = useTranslation()
  const { copyToClipboard } = useCopyToClipboard()
  const query = useQuery({
    queryKey: ['request-log-detail', props.detailId],
    queryFn: async () => {
      const result = await getLogDetail(props.detailId)
      if (!result.success || !result.data) {
        throw new Error(result.message || t('Failed to load log details'))
      }
      return result.data
    },
  })

  if (query.isLoading) return <div className='p-6'>{t('Loading...')}</div>
  if (query.isError || !query.data) {
    return (
      <div className='space-y-4 p-6'>
        <Button variant='outline' onClick={props.onBack}>
          <ArrowLeft className='mr-2 size-4' />
          {t('Back')}
        </Button>
        <p className='text-destructive'>{t('Failed to load log details')}</p>
      </div>
    )
  }

  const detail = query.data
  return (
    <main className='flex h-full min-h-0 flex-col overflow-auto p-4 sm:p-6'>
      <Tabs defaultValue='request' className='flex min-h-0 flex-1 flex-col'>
        <header className='flex min-w-0 flex-wrap items-start gap-3'>
          <Button
            variant='outline'
            size='icon-sm'
            onClick={props.onBack}
            aria-label={t('Back')}
          >
            <ArrowLeft className='size-4' />
          </Button>
          <div className='min-w-0 flex-1'>
            <div className='flex min-w-0 items-center gap-1'>
              <h1 className='text-lg font-semibold'>
                {t('Request Log Details')}
              </h1>
              <span className='text-muted-foreground min-w-0 truncate font-mono text-xs'>
                {detail.id}
              </span>
              <Button
                variant='ghost'
                size='icon-sm'
                onClick={() => copyToClipboard(detail.id)}
                aria-label={t('Copy ID')}
                title={t('Copy ID')}
              >
                <Copy className='size-4' />
              </Button>
            </div>
            <div className='text-muted-foreground flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 font-mono text-xs'>
              <span className='text-foreground font-semibold'>
                {detail.request_method || '-'}
              </span>
              <span>{detail.status_code || '-'}</span>
              <span>{detail.duration_ms} ms</span>
              <span className='truncate'>{detail.request_path || '-'}</span>
            </div>
          </div>
          <TabsList className='grid w-full shrink-0 grid-cols-2 sm:w-48'>
            <TabsTrigger value='request'>{t('Request')}</TabsTrigger>
            <TabsTrigger value='response'>{t('Response')}</TabsTrigger>
          </TabsList>
        </header>
        <TabsContent value='request' className='min-h-0 flex-1'>
          <div className='flex min-h-0 flex-col gap-4'>
            <HeaderPreview
              title={t('Request Headers')}
              value={detail.request_headers}
              collapsible
            />
            <PayloadPanel
              title={t('Request Body')}
              body={detail.request_body}
              contentType={detail.request_content_type}
              truncated={detail.request_truncated}
            />
          </div>
        </TabsContent>
        <TabsContent value='response' className='min-h-0 flex-1'>
          <div className='flex min-h-0 flex-col gap-4'>
            <HeaderPreview
              title={t('Response Headers')}
              value={detail.response_headers}
              collapsible
            />
            <PayloadPanel
              title={t('Response Body')}
              body={detail.response_body || detail.error_body}
              contentType={detail.response_content_type}
              truncated={detail.response_truncated}
            />
          </div>
        </TabsContent>
      </Tabs>
    </main>
  )
}
