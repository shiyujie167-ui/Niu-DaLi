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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Video, Wallet } from 'lucide-react'
import { lazy, Suspense, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { LoadingState } from '@/components/loading-state'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { getServerErrorMessage } from '@/lib/server-error-message'

import { createVideoTask, getVideoHistory, getVideoModels } from './api'
import { VideoForm } from './components/video-form'
import { VideoHistory } from './components/video-history'

const CATALOG_QUERY_KEY = ['video-workspace', 'models']
const HISTORY_QUERY_KEY = ['video-workspace', 'tasks']
const VideoCanvas = lazy(() => import('./canvas'))

export function VideoWorkspace() {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 10 })
  const [mode, setMode] = useState('standard')
  const [canvasOpened, setCanvasOpened] = useState(false)
  const catalogQuery = useQuery({
    queryKey: CATALOG_QUERY_KEY,
    queryFn: getVideoModels,
    refetchInterval: 15_000,
    retry: false,
    meta: { errorToast: false },
  })
  const historyQuery = useQuery({
    queryKey: [...HISTORY_QUERY_KEY, pagination],
    queryFn: () =>
      getVideoHistory(pagination.pageIndex + 1, pagination.pageSize),
    refetchInterval: 5_000,
    retry: false,
    meta: { errorToast: false },
  })
  const submitMutation = useMutation({
    mutationFn: createVideoTask,
    retry: false,
    meta: { errorToast: false },
    onSuccess: async () => {
      toast.success(t('Video task submitted'))
      setPagination((previous) => ({ ...previous, pageIndex: 0 }))
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: HISTORY_QUERY_KEY }),
        queryClient.invalidateQueries({ queryKey: CATALOG_QUERY_KEY }),
      ])
    },
    onError: () => {
      // The upstream may have accepted a task before a response was interrupted.
      // Never replay the submission automatically; refresh the persisted state.
      void queryClient.invalidateQueries({ queryKey: HISTORY_QUERY_KEY })
      void queryClient.invalidateQueries({ queryKey: CATALOG_QUERY_KEY })
    },
  })

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Video workspace')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <span className='text-muted-foreground text-sm'>
          {t('Balance')}:{' '}
          <span className='text-foreground font-medium'>
            {formatQuotaWithCurrency(catalogQuery.data?.quota, { locale })}
          </span>
        </span>
        <Button
          variant='outline'
          size='sm'
          nativeButton={false}
          render={<Link to='/wallet' />}
        >
          <Wallet data-icon='inline-start' />
          {t('Wallet')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <Tabs
          value={mode}
          onValueChange={(value) => {
            setMode(String(value))
            if (value === 'canvas') setCanvasOpened(true)
          }}
          className='gap-4'
        >
          <TabsList aria-label={t('Video workspace mode')}>
            <TabsTrigger value='standard'>{t('Standard')}</TabsTrigger>
            <TabsTrigger value='canvas'>{t('Infinite canvas')}</TabsTrigger>
          </TabsList>
          <TabsContent value='standard' keepMounted>
            <div className='mx-auto grid w-full max-w-7xl gap-6 lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)] lg:items-start'>
              <Card className='min-w-0'>
                <CardHeader>
                  <CardTitle>{t('Create a video')}</CardTitle>
                  <CardDescription>
                    {t(
                      'Choose an available model and bring your idea to life.'
                    )}
                  </CardDescription>
                </CardHeader>
                <CardContent className='space-y-4'>
                  {catalogQuery.isPending && <LoadingState />}
                  {catalogQuery.isError && (
                    <ErrorState
                      title={t('Failed to load video models')}
                      description={getServerErrorMessage(catalogQuery.error)}
                      onRetry={() => void catalogQuery.refetch()}
                    />
                  )}
                  {catalogQuery.isSuccess &&
                    catalogQuery.data.models.length === 0 && (
                      <EmptyState
                        icon={Video}
                        title={t('No video channels available')}
                        description={t(
                          'Ask your administrator to configure a supported video channel and model pricing for your group.'
                        )}
                        className='min-h-48'
                      />
                    )}
                  {catalogQuery.isSuccess &&
                    catalogQuery.data.models.length > 0 && (
                      <VideoForm
                        catalog={catalogQuery.data}
                        pending={submitMutation.isPending}
                        onSubmit={(submission) =>
                          submitMutation.mutate(submission)
                        }
                      />
                    )}
                  {submitMutation.isError && (
                    <Alert variant='destructive'>
                      <AlertTitle>
                        {t('Failed to submit video task')}
                      </AlertTitle>
                      <AlertDescription>
                        <p>{getServerErrorMessage(submitMutation.error)}</p>
                        <p>
                          {t(
                            'Check your video history before submitting again if the connection was interrupted.'
                          )}
                        </p>
                      </AlertDescription>
                    </Alert>
                  )}
                </CardContent>
              </Card>
              <VideoHistory
                query={historyQuery}
                pagination={pagination}
                onPaginationChange={setPagination}
              />
            </div>
          </TabsContent>
          <TabsContent value='canvas' keepMounted>
            {canvasOpened && (
              <Suspense fallback={<LoadingState className='min-h-80' />}>
                {catalogQuery.isError && (
                  <ErrorState
                    title={t('Failed to load video models')}
                    description={getServerErrorMessage(catalogQuery.error)}
                    onRetry={() => void catalogQuery.refetch()}
                  />
                )}
                <VideoCanvas
                  catalog={catalogQuery.data ?? { models: [], quota: 0 }}
                />
              </Suspense>
            )}
          </TabsContent>
        </Tabs>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
