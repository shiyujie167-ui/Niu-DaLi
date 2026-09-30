/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type { UseQueryResult } from '@tanstack/react-query'
import type { OnChangeFn, PaginationState } from '@tanstack/react-table'
import { RefreshCw, Video } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { DataTablePagination, useDataTable } from '@/components/data-table'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { TaskArtifactsCell } from '@/features/usage-logs/components/task-artifacts'
import { taskStatusMapper } from '@/features/usage-logs/lib/mappers'
import type { TaskLog } from '@/features/usage-logs/types'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { formatTimestampToDate } from '@/lib/format'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { getVideoArtifacts, getVideoMedia } from '../api'
import type { VideoWorkspaceHistory } from '../types'

const EMPTY_TASKS: TaskLog[] = []

interface VideoHistoryProps {
  query: UseQueryResult<VideoWorkspaceHistory, Error>
  pagination: PaginationState
  onPaginationChange: OnChangeFn<PaginationState>
}

export function VideoHistory(props: VideoHistoryProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const tasks = props.query.data?.items ?? EMPTY_TASKS
  const { table } = useDataTable({
    data: tasks,
    columns: [],
    totalCount: props.query.data?.total ?? 0,
    pagination: props.pagination,
    onPaginationChange: props.onPaginationChange,
    manualPagination: true,
    columnVisibilityStorageKey: false,
    columnSizingStorageKey: false,
  })
  return (
    <section
      className='min-w-0 space-y-4'
      aria-labelledby='video-history-title'
    >
      <div className='flex items-center justify-between gap-2'>
        <h3 id='video-history-title' className='text-base font-semibold'>
          {t('Video history')}
        </h3>
        <Button
          variant='outline'
          size='sm'
          disabled={props.query.isFetching}
          onClick={() => void props.query.refetch()}
        >
          <RefreshCw data-icon='inline-start' />
          {t('Refresh')}
        </Button>
      </div>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Your video tasks are saved to your account. Active tasks update automatically.'
        )}
      </p>
      {props.query.isPending && <LoadingState />}
      {props.query.isError && (
        <ErrorState
          title={t('Failed to load video history')}
          description={getServerErrorMessage(props.query.error)}
          onRetry={() => void props.query.refetch()}
        />
      )}
      {props.query.isSuccess && tasks.length === 0 && (
        <EmptyState
          icon={Video}
          title={t('No videos yet')}
          description={t(
            'Submit a video task to see its progress and result here.'
          )}
          bordered
        />
      )}
      {props.query.isSuccess && tasks.length > 0 && (
        <>
          <div className='grid min-w-0 gap-3' aria-live='polite'>
            {tasks.map((task) => (
              <Card key={task.task_id} size='sm'>
                <CardHeader>
                  <div className='flex flex-wrap items-start justify-between gap-2'>
                    <CardTitle className='min-w-0 break-all'>
                      {task.properties?.origin_model_name ||
                        task.properties?.upstream_model_name ||
                        task.platform_name ||
                        task.platform}
                    </CardTitle>
                    <StatusBadge
                      label={t(taskStatusMapper.getLabel(task.status))}
                      variant={taskStatusMapper.getVariant(task.status)}
                      copyable={false}
                    />
                  </div>
                  <p className='text-muted-foreground text-xs'>
                    {formatTimestampToDate(task.submit_time, 'seconds')}
                  </p>
                </CardHeader>
                <CardContent className='space-y-2'>
                  {task.properties?.input && (
                    <p className='line-clamp-3 text-sm break-words whitespace-pre-wrap'>
                      {task.properties.input}
                    </p>
                  )}
                  <p className='text-muted-foreground font-mono text-xs break-all'>
                    {task.task_id}
                  </p>
                  {task.progress &&
                    task.status !== 'SUCCESS' &&
                    task.status !== 'FAILURE' && (
                      <p className='text-muted-foreground text-xs'>
                        {t('Progress')}: {task.progress}
                      </p>
                    )}
                  {task.fail_reason && (
                    <p className='text-destructive text-sm break-words'>
                      {task.fail_reason}
                    </p>
                  )}
                </CardContent>
                <CardFooter className='flex flex-wrap items-center justify-between gap-2'>
                  <span className='text-muted-foreground text-xs'>
                    {t('Task charge')}:{' '}
                    {formatQuotaWithCurrency(task.quota, { locale })}
                  </span>
                  <TaskArtifactsCell
                    log={task}
                    loadArtifacts={getVideoArtifacts}
                    loadMedia={getVideoMedia}
                    artifactQueryKey='video-workspace'
                  />
                </CardFooter>
              </Card>
            ))}
          </div>
          <DataTablePagination table={table} compact />
        </>
      )}
    </section>
  )
}
