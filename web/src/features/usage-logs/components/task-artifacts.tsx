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
import {
  Alert02Icon,
  Download01Icon,
  File01Icon,
  Image01Icon,
  MusicNote01Icon,
  RefreshIcon,
  Video01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useQuery } from '@tanstack/react-query'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Field, FieldLabel } from '@/components/ui/field'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { requireServerSuccess } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import { getTaskArtifacts } from '../api'
import {
  resolveTaskPreviewMode,
  shouldLoadTaskArtifacts,
} from '../lib/task-artifacts'
import {
  useTaskMediaUrl,
  type TaskMediaLoader,
} from '../lib/use-task-media-url'
import type { TaskArtifact, TaskArtifactType, TaskLog } from '../types'
import { AudioPreviewDialog } from './dialogs/audio-preview-dialog'

function artifactIcon(type: TaskArtifactType) {
  switch (type) {
    case 'image':
      return Image01Icon
    case 'video':
      return Video01Icon
    case 'audio':
      return MusicNote01Icon
    case 'file':
      return File01Icon
  }
}

function artifactTypeLabel(type: TaskArtifactType): string {
  switch (type) {
    case 'image':
      return 'Image'
    case 'video':
      return 'Video'
    case 'audio':
      return 'Audio'
    case 'file':
      return 'File'
  }
}

// Task lists no longer carry the persisted snapshot, so the legacy Suno clip
// list is fetched through the artifacts endpoint when the preview opens.
interface ArtifactSourceProps {
  loadArtifacts?: typeof getTaskArtifacts
  artifactQueryKey?: string
  loadMedia?: TaskMediaLoader
}

function LegacyAudioPreview(props: { taskId: string } & ArtifactSourceProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const artifactsQuery = useQuery({
    queryKey: [
      props.artifactQueryKey ?? 'usage-logs',
      'task-artifacts',
      props.taskId,
    ],
    queryFn: async () =>
      requireServerSuccess(
        await (props.loadArtifacts ?? getTaskArtifacts)(props.taskId)
      ),
    enabled: open,
    retry: false,
    staleTime: 30_000,
  })

  return (
    <>
      <button
        type='button'
        className='group flex items-center gap-1 text-left text-xs'
        onClick={() => setOpen(true)}
      >
        <HugeiconsIcon
          icon={MusicNote01Icon}
          className='text-muted-foreground size-3'
          strokeWidth={2}
          aria-hidden='true'
        />
        <span className='text-foreground leading-snug group-hover:underline'>
          {t('Click to preview audio')}
        </span>
      </button>
      <AudioPreviewDialog
        open={open}
        onOpenChange={setOpen}
        clips={artifactsQuery.data?.legacyAudioClips ?? []}
        loading={open && artifactsQuery.isPending}
      />
    </>
  )
}

function ArtifactMedia(props: {
  artifact: TaskArtifact
  mediaUrl: string
  onError: () => void
}) {
  if (props.artifact.type === 'image') {
    return (
      <img
        src={props.mediaUrl}
        alt={props.artifact.key}
        loading='lazy'
        className='max-h-[60vh] w-full rounded-md object-contain'
        onError={props.onError}
      />
    )
  }
  if (props.artifact.type === 'video') {
    return (
      <video
        src={props.mediaUrl}
        controls
        preload='metadata'
        className='max-h-[60vh] w-full rounded-md bg-black'
        onError={props.onError}
      />
    )
  }
  if (props.artifact.type === 'audio') {
    return (
      <audio
        src={props.mediaUrl}
        controls
        preload='none'
        className='w-full'
        onError={props.onError}
      />
    )
  }
  return null
}

function MediaFailure(props: { onRetry: () => void }) {
  const { t } = useTranslation()
  return (
    <Alert variant='destructive'>
      <HugeiconsIcon icon={Alert02Icon} strokeWidth={2} aria-hidden='true' />
      <AlertTitle>{t('Media preview failed. Please try again.')}</AlertTitle>
      <AlertDescription>{t('Preview unavailable')}</AlertDescription>
      <AlertAction>
        <Button
          type='button'
          variant='outline'
          size='xs'
          onClick={props.onRetry}
        >
          <HugeiconsIcon
            icon={RefreshIcon}
            strokeWidth={2}
            data-icon='inline-start'
          />
          {t('Retry')}
        </Button>
      </AlertAction>
    </Alert>
  )
}

function TaskArtifactCard(props: {
  artifact: TaskArtifact
  loadMedia?: TaskMediaLoader
}) {
  const { t } = useTranslation()
  const [mediaFailed, setMediaFailed] = useState(false)
  const [mediaRevision, setMediaRevision] = useState(0)
  const media = useTaskMediaUrl(
    props.artifact.content_url,
    mediaRevision,
    props.loadMedia
  )
  const icon = artifactIcon(props.artifact.type)
  const isVisualArtifact =
    props.artifact.type === 'image' || props.artifact.type === 'video'

  let cardContent = (
    <div
      className={cn(
        'bg-muted/40 text-muted-foreground flex items-center justify-center rounded-md',
        isVisualArtifact ? 'aspect-video min-h-48' : 'min-h-20'
      )}
    >
      <HugeiconsIcon icon={icon} className='size-6' strokeWidth={1.5} />
    </div>
  )
  if (mediaFailed || media.failed) {
    cardContent = (
      <MediaFailure
        onRetry={() => {
          setMediaFailed(false)
          setMediaRevision((revision) => revision + 1)
        }}
      />
    )
  } else if (media.loading) {
    cardContent = (
      <div aria-label={t('Loading...')}>
        <Skeleton className='aspect-video min-h-48 w-full rounded-xl' />
      </div>
    )
  } else if (props.artifact.type !== 'file' && media.url) {
    cardContent = (
      <ArtifactMedia
        key={mediaRevision}
        artifact={props.artifact}
        mediaUrl={media.url}
        onError={() => setMediaFailed(true)}
      />
    )
  }

  return (
    <Card size='sm'>
      <CardHeader>
        <CardTitle className='flex min-w-0 items-center gap-1.5'>
          <HugeiconsIcon
            icon={icon}
            className='size-4 shrink-0'
            strokeWidth={2}
            aria-hidden='true'
          />
          <span className='truncate'>
            {t(artifactTypeLabel(props.artifact.type))}
          </span>
        </CardTitle>
        <CardDescription className='min-w-0'>
          <span className='block truncate font-mono text-xs'>
            {props.artifact.key}
          </span>
          {props.artifact.mime_type ? (
            <span className='block truncate font-mono text-[11px]'>
              {props.artifact.mime_type}
            </span>
          ) : null}
        </CardDescription>
      </CardHeader>
      <CardContent>{cardContent}</CardContent>
      <CardFooter>
        <Button
          variant='outline'
          size='sm'
          nativeButton={false}
          disabled={!media.url}
          render={
            <a
              href={media.url}
              download={props.artifact.key}
              target='_blank'
              rel='noopener noreferrer'
            />
          }
        >
          <HugeiconsIcon
            icon={Download01Icon}
            strokeWidth={2}
            data-icon='inline-start'
          />
          {t('Download')}
        </Button>
      </CardFooter>
    </Card>
  )
}

interface TaskArtifactsProps extends ArtifactSourceProps {
  taskId: string
  enabled: boolean
  emptyContent?: (legacyContentUrl?: string) => React.ReactNode
}

function TaskArtifacts(props: TaskArtifactsProps) {
  const { t } = useTranslation()
  const artifactSelectId = useId()
  const [selectedKey, setSelectedKey] = useState('')
  const artifactsQuery = useQuery({
    queryKey: [
      props.artifactQueryKey ?? 'usage-logs',
      'task-artifacts',
      props.taskId,
    ],
    queryFn: async () =>
      requireServerSuccess(
        await (props.loadArtifacts ?? getTaskArtifacts)(props.taskId)
      ),
    enabled: props.enabled,
    retry: false,
    staleTime: 30_000,
  })

  if (!props.enabled) return null

  if (artifactsQuery.isPending) {
    return (
      <div aria-label={t('Loading...')}>
        <Skeleton className='aspect-video min-h-48 w-full rounded-xl' />
      </div>
    )
  }

  if (artifactsQuery.isError) {
    return (
      <Alert variant='destructive'>
        <HugeiconsIcon icon={Alert02Icon} strokeWidth={2} aria-hidden='true' />
        <AlertTitle>{t('Failed to load artifacts')}</AlertTitle>
        <AlertDescription>{t('Preview unavailable')}</AlertDescription>
        <AlertAction>
          <Button
            type='button'
            variant='outline'
            size='xs'
            disabled={artifactsQuery.isFetching}
            onClick={() => void artifactsQuery.refetch()}
          >
            {artifactsQuery.isFetching ? (
              <Spinner data-icon='inline-start' />
            ) : (
              <HugeiconsIcon
                icon={RefreshIcon}
                strokeWidth={2}
                data-icon='inline-start'
              />
            )}
            {t('Retry')}
          </Button>
        </AlertAction>
      </Alert>
    )
  }

  if (artifactsQuery.data.artifacts.length === 0) {
    return (
      props.emptyContent?.(artifactsQuery.data.legacyContentUrl) ?? (
        <EmptyTaskArtifacts />
      )
    )
  }

  if (props.loadMedia) {
    const artifacts = artifactsQuery.data.artifacts
    const artifact =
      artifacts.find((item) => item.key === selectedKey) ?? artifacts[0]
    return (
      <div className='space-y-3'>
        {artifacts.length > 1 && (
          <Field>
            <FieldLabel htmlFor={artifactSelectId}>{t('Artifacts')}</FieldLabel>
            <NativeSelect
              id={artifactSelectId}
              value={artifact.key}
              onChange={(event) => setSelectedKey(event.target.value)}
            >
              {artifacts.map((item) => (
                <NativeSelectOption key={item.key} value={item.key}>
                  {item.key}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </Field>
        )}
        <TaskArtifactCard
          key={artifact.key}
          artifact={artifact}
          loadMedia={props.loadMedia}
        />
      </div>
    )
  }

  return (
    <div
      className={cn(
        'grid gap-3',
        artifactsQuery.data.artifacts.length > 1 && 'lg:grid-cols-2'
      )}
    >
      {artifactsQuery.data.artifacts.map((artifact) => (
        <TaskArtifactCard key={artifact.key} artifact={artifact} />
      ))}
    </div>
  )
}

function EmptyTaskArtifacts() {
  const { t } = useTranslation()
  return (
    <Empty>
      <EmptyHeader>
        <EmptyMedia variant='icon'>
          <HugeiconsIcon icon={File01Icon} strokeWidth={2} aria-hidden='true' />
        </EmptyMedia>
        <EmptyTitle>{t('Artifacts')}</EmptyTitle>
        <EmptyDescription>{t('None')}</EmptyDescription>
      </EmptyHeader>
    </Empty>
  )
}

function LegacyTaskArtifacts(props: {
  legacyContentUrl?: string
  loadMedia?: TaskMediaLoader
}) {
  if (props.legacyContentUrl) {
    return (
      <LegacyVideoMedia
        contentUrl={props.legacyContentUrl}
        loadMedia={props.loadMedia}
      />
    )
  }
  return <EmptyTaskArtifacts />
}

export function TaskArtifactsCell(
  props: { log: TaskLog } & ArtifactSourceProps
) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const previewMode = resolveTaskPreviewMode(props.log)

  if (previewMode === 'discarded') {
    return (
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger
            render={
              <span className='text-muted-foreground cursor-help text-xs' />
            }
          >
            {t('Result not retained')}
          </TooltipTrigger>
          <TooltipContent>
            {t(
              'The result was returned in the API response and was not saved as an artifact'
            )}
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
    )
  }
  if (!shouldLoadTaskArtifacts(props.log, true)) {
    return <span className='text-muted-foreground/60 text-xs'>-</span>
  }
  if (previewMode === 'legacy-suno') {
    return (
      <LegacyAudioPreview
        taskId={props.log.task_id}
        loadArtifacts={props.loadArtifacts}
        artifactQueryKey={props.artifactQueryKey}
      />
    )
  }

  return (
    <>
      {previewMode === 'legacy-video' ? (
        <button
          type='button'
          className='text-foreground text-xs hover:underline'
          onClick={() => setOpen(true)}
        >
          {t('Click to preview video')}
        </button>
      ) : (
        <Button
          type='button'
          variant='outline'
          size='xs'
          onClick={() => setOpen(true)}
        >
          <HugeiconsIcon
            icon={File01Icon}
            strokeWidth={2}
            data-icon='inline-start'
          />
          {t('Artifacts')}
        </Button>
      )}
      <Dialog
        open={open}
        onOpenChange={setOpen}
        title={
          previewMode === 'legacy-video' ? (
            t('Preview')
          ) : (
            <span className='flex items-center gap-2'>
              <HugeiconsIcon
                icon={File01Icon}
                className='text-muted-foreground size-4'
                strokeWidth={2}
                aria-hidden='true'
              />
              {t('Artifacts')}
            </span>
          )
        }
        contentClassName={
          previewMode === 'legacy-video' ? 'sm:max-w-xl' : 'sm:max-w-4xl'
        }
        contentHeight='auto'
        bodyClassName='pr-2 sm:pr-4'
      >
        <TaskArtifacts
          taskId={props.log.task_id}
          loadArtifacts={props.loadArtifacts}
          artifactQueryKey={props.artifactQueryKey}
          loadMedia={props.loadMedia}
          enabled={shouldLoadTaskArtifacts(props.log, open)}
          emptyContent={(legacyContentUrl) => (
            <LegacyTaskArtifacts
              legacyContentUrl={legacyContentUrl}
              loadMedia={props.loadMedia}
            />
          )}
        />
      </Dialog>
    </>
  )
}

interface LegacyVideoMediaProps {
  contentUrl: string
  loadMedia?: TaskMediaLoader
}

function LegacyVideoMedia(props: LegacyVideoMediaProps) {
  const { t } = useTranslation()
  const [mediaFailed, setMediaFailed] = useState(false)
  const [mediaRevision, setMediaRevision] = useState(0)

  const media = useTaskMediaUrl(
    props.contentUrl,
    mediaRevision,
    props.loadMedia
  )
  if (media.loading) {
    return (
      <div aria-label={t('Loading...')}>
        <Skeleton className='aspect-video min-h-48 w-full rounded-xl' />
      </div>
    )
  }

  return mediaFailed || media.failed ? (
    <MediaFailure
      onRetry={() => {
        setMediaFailed(false)
        setMediaRevision((revision) => revision + 1)
      }}
    />
  ) : (
    <div className='space-y-3'>
      <video
        key={mediaRevision}
        src={media.url}
        controls
        preload='metadata'
        className='max-h-[60vh] w-full rounded-md bg-black'
        onError={() => setMediaFailed(true)}
      />
      <Button
        variant='outline'
        size='sm'
        nativeButton={false}
        render={
          <a
            href={media.url}
            download='video.mp4'
            target='_blank'
            rel='noopener noreferrer'
          />
        }
      >
        <HugeiconsIcon
          icon={Download01Icon}
          strokeWidth={2}
          data-icon='inline-start'
        />
        {t('Download')}
      </Button>
    </div>
  )
}
