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
import { Handle, Position, type NodeProps } from '@xyflow/react'
import { Image, Type, Video, X } from 'lucide-react'
import { memo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'
import { taskStatusMapper } from '@/features/usage-logs/lib/mappers'
import { useTaskMediaUrl } from '@/features/usage-logs/lib/use-task-media-url'
import { getServerErrorMessage } from '@/lib/server-error-message'
import { cn } from '@/lib/utils'

import { getVideoParameterDefaults } from '../lib/parameters'
import { loadCanvasImage } from './api'
import { CanvasResults } from './canvas-results'
import { useCanvasActions } from './context'
import { nodeTaskId } from './graph'
import type { CanvasNode } from './types'

function ImagePreview(props: { url: string; filename: string }) {
  const { t } = useTranslation()
  const [revision, setRevision] = useState(0)
  const media = useTaskMediaUrl(props.url, revision, loadCanvasImage)
  if (media.loading) {
    return (
      <div className='flex h-40 items-center justify-center'>
        <Spinner aria-label={t('Loading...')} />
      </div>
    )
  }
  if (media.failed) {
    return (
      <Button
        variant='outline'
        onClick={() => setRevision((value) => value + 1)}
      >
        {t('Retry')}
      </Button>
    )
  }
  return (
    <img
      src={media.url}
      alt={props.filename}
      className='max-h-52 w-full rounded-xl object-contain'
    />
  )
}

function CanvasNodeCard(props: NodeProps<CanvasNode>) {
  const { t } = useTranslation()
  const actions = useCanvasActions()
  const model = actions.catalog.models.find(
    (item) => item.id === props.data.model
  )
  const submission = actions.submissions.get(props.id)
  const taskId = nodeTaskId(
    {
      id: props.id,
      data: props.data,
      position: { x: 0, y: 0 },
      type: props.type,
    },
    actions.submissions
  )
  const task = taskId ? actions.tasks.get(taskId) : undefined
  const pendingClaim =
    submission?.status === 'submitting' || submission?.status === 'unknown'
  const activeTask = Boolean(
    taskId && (!task || !['SUCCESS', 'FAILURE'].includes(task.status))
  )
  const busy = actions.busyNodes.has(props.id)
  const locked = actions.locked || busy || pendingClaim || activeTask
  let title = t('Prompt node')
  let Icon = Type
  if (props.type === 'image') {
    title = t('Image asset')
    Icon = Image
  }
  if (props.type === 'generation') {
    title = t('Video generation')
    Icon = Video
  }
  return (
    <article
      className={cn(
        'w-80 rounded-2xl border border-neutral-200 bg-white text-neutral-900 shadow-sm dark:border-neutral-700 dark:bg-neutral-950 dark:text-neutral-100',
        props.selected && 'ring-2 ring-blue-500'
      )}
      aria-label={title}
    >
      {props.type === 'generation' && (
        <Handle
          type='target'
          position={Position.Left}
          id='input'
          className='!size-3 !border-2 !border-white !bg-neutral-400'
          aria-label={t('Connect input')}
        />
      )}
      <Handle
        type='source'
        position={Position.Right}
        id='output'
        className='!size-3 !border-2 !border-white !bg-blue-500'
        aria-label={t('Connect output')}
      />
      <header className='flex items-center gap-2 border-b border-neutral-100 px-4 py-3 dark:border-neutral-800'>
        <Icon className='size-4 text-neutral-500' aria-hidden='true' />
        <h3 className='flex-1 text-sm font-medium'>{title}</h3>
        <Button
          className='nodrag nopan'
          variant='ghost'
          size='icon-xs'
          aria-label={t('Delete node')}
          disabled={busy || actions.locked}
          onClick={() => actions.deleteNode(props.id)}
        >
          <X className='size-3.5' aria-hidden='true' />
        </Button>
      </header>
      <div className='nodrag nopan nowheel space-y-3 p-4'>
        {props.type === 'prompt' && (
          <Field>
            <FieldLabel htmlFor={`${props.id}-prompt`}>
              {t('Video prompt')}
            </FieldLabel>
            <Textarea
              id={`${props.id}-prompt`}
              disabled={actions.locked}
              value={props.data.prompt ?? ''}
              maxLength={16000}
              rows={5}
              placeholder={t('Describe the scene, movement, and visual style')}
              onChange={(event) =>
                actions.updateNode(props.id, { prompt: event.target.value })
              }
            />
          </Field>
        )}
        {props.type === 'image' && (
          <>
            {props.data.content_url && (
              <ImagePreview
                url={props.data.content_url}
                filename={props.data.filename ?? t('Reference image')}
              />
            )}
            <Field>
              <FieldLabel htmlFor={`${props.id}-upload`}>
                {t('Reference image')}
              </FieldLabel>
              <Input
                id={`${props.id}-upload`}
                type='file'
                accept='image/png,image/jpeg,image/webp'
                disabled={busy || actions.locked}
                onChange={(event) => {
                  const file = event.target.files?.[0]
                  if (file) void actions.uploadImage(props.id, file)
                  event.target.value = ''
                }}
              />
            </Field>
            {busy && <Spinner aria-label={t('Uploading...')} />}
            {props.data.filename && (
              <p className='truncate text-xs text-neutral-500'>
                {props.data.filename}
              </p>
            )}
            <p className='text-xs text-neutral-500'>
              {t('Connect this image to a video generation node.')}
            </p>
          </>
        )}
        {props.type === 'generation' && (
          <>
            <fieldset disabled={locked} className='space-y-3'>
              <Field>
                <FieldLabel htmlFor={`${props.id}-model`}>
                  {t('Model')}
                </FieldLabel>
                <NativeSelect
                  id={`${props.id}-model`}
                  value={props.data.model ?? ''}
                  className='w-full'
                  onChange={(event) => {
                    const next = actions.catalog.models.find(
                      (item) => item.id === event.target.value
                    )
                    if (next) {
                      actions.updateNode(props.id, {
                        model: next.id,
                        ...getVideoParameterDefaults(next),
                        count: 1,
                      })
                    }
                  }}
                >
                  {!model && (
                    <NativeSelectOption value={props.data.model ?? ''}>
                      {t('Choose an available video model')}
                    </NativeSelectOption>
                  )}
                  {actions.catalog.models.map((item) => (
                    <NativeSelectOption key={item.id} value={item.id}>
                      {item.name}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
              </Field>
              <Field>
                <FieldLabel htmlFor={`${props.id}-prompt`}>
                  {t('Video prompt')}
                </FieldLabel>
                <Textarea
                  id={`${props.id}-prompt`}
                  rows={3}
                  maxLength={model?.max_prompt_length ?? 4000}
                  value={props.data.prompt ?? ''}
                  placeholder={t('Add instructions or connect a prompt node')}
                  onChange={(event) =>
                    actions.updateNode(props.id, { prompt: event.target.value })
                  }
                />
              </Field>
              <div className='grid grid-cols-2 gap-3'>
                {Boolean(model?.durations?.length) && (
                  <Field>
                    <FieldLabel htmlFor={`${props.id}-seconds`}>
                      {t('Video duration')}
                    </FieldLabel>
                    <NativeSelect
                      id={`${props.id}-seconds`}
                      value={props.data.seconds ?? ''}
                      onChange={(event) =>
                        actions.updateNode(props.id, {
                          seconds: event.target.value,
                        })
                      }
                    >
                      {model?.durations?.map((value) => (
                        <NativeSelectOption key={value} value={String(value)}>
                          {t('{{count}} seconds', { count: value })}
                        </NativeSelectOption>
                      ))}
                    </NativeSelect>
                  </Field>
                )}
                {Boolean(model?.sizes?.length) && (
                  <Field>
                    <FieldLabel htmlFor={`${props.id}-size`}>
                      {t('Video size')}
                    </FieldLabel>
                    <NativeSelect
                      id={`${props.id}-size`}
                      value={props.data.size ?? ''}
                      onChange={(event) =>
                        actions.updateNode(props.id, {
                          size: event.target.value,
                        })
                      }
                    >
                      {model?.sizes?.map((value) => (
                        <NativeSelectOption key={value} value={value}>
                          {value}
                        </NativeSelectOption>
                      ))}
                    </NativeSelect>
                  </Field>
                )}
                {Boolean(model?.resolutions?.length) && (
                  <Field>
                    <FieldLabel htmlFor={`${props.id}-resolution`}>
                      {t('Video resolution')}
                    </FieldLabel>
                    <NativeSelect
                      id={`${props.id}-resolution`}
                      value={props.data.resolution ?? ''}
                      onChange={(event) =>
                        actions.updateNode(props.id, {
                          resolution: event.target.value,
                        })
                      }
                    >
                      <NativeSelectOption value=''>
                        {t('Default')}
                      </NativeSelectOption>
                      {model?.resolutions?.map((value) => (
                        <NativeSelectOption key={value} value={value}>
                          {value}
                        </NativeSelectOption>
                      ))}
                    </NativeSelect>
                  </Field>
                )}
                <Field>
                  <FieldLabel htmlFor={`${props.id}-count`}>
                    {t('Quantity')}
                  </FieldLabel>
                  <Input
                    id={`${props.id}-count`}
                    type='number'
                    min={1}
                    max={model?.max_outputs ?? 1}
                    step={1}
                    value={props.data.count ?? 1}
                    onChange={(event) =>
                      actions.updateNode(props.id, {
                        count: Number(event.target.value),
                      })
                    }
                  />
                </Field>
              </div>
              {model && (
                <p className='text-xs text-neutral-500'>
                  {t('Reference limits: {{images}} images, {{videos}} videos', {
                    images:
                      model.max_reference_images ??
                      (model.supports_image ? 1 : 0),
                    videos: model.max_reference_videos ?? 0,
                  })}
                </p>
              )}
              <Button
                className='w-full'
                disabled={locked || !model || actions.catalog.quota <= 0}
                onClick={() => void actions.generate(props.id)}
              >
                {busy && <Spinner data-icon='inline-start' />}
                {busy ? t('Submitting...') : t('Generate video')}
              </Button>
            </fieldset>
            {actions.catalog.quota <= 0 && (
              <p role='status' className='text-destructive text-xs'>
                {t('Insufficient balance. Add funds to generate a video.')}
              </p>
            )}
            {pendingClaim && (
              <div role='status' className='space-y-2 text-xs text-neutral-500'>
                <p>
                  {t(
                    'Submission is being reconciled. It will not be sent again.'
                  )}
                </p>
                <Button variant='outline' size='sm' onClick={actions.refresh}>
                  {t('Refresh')}
                </Button>
              </div>
            )}
            {actions.errors.has(props.id) && (
              <p role='alert' className='text-destructive text-xs'>
                {getServerErrorMessage(actions.errors.get(props.id))}
              </p>
            )}
            {taskId && actions.taskErrors.has(taskId) && (
              <div role='alert' className='space-y-2 text-xs'>
                <p>{getServerErrorMessage(actions.taskErrors.get(taskId))}</p>
                <Button variant='outline' size='sm' onClick={actions.refresh}>
                  {t('Retry')}
                </Button>
              </div>
            )}
            {task && (
              <div aria-live='polite' className='space-y-2'>
                <StatusBadge
                  label={t(taskStatusMapper.getLabel(task.status))}
                  variant={taskStatusMapper.getVariant(task.status)}
                  copyable={false}
                />
                {task.progress && task.status !== 'SUCCESS' && (
                  <p className='text-xs'>
                    {t('Progress')}: {task.progress}
                  </p>
                )}
                {task.fail_reason && (
                  <p className='text-destructive text-xs'>{task.fail_reason}</p>
                )}
                <p className='truncate font-mono text-[10px] text-neutral-500'>
                  {task.task_id}
                </p>
              </div>
            )}
            {task?.status === 'SUCCESS' && taskId && (
              <CanvasResults id={props.id} data={props.data} taskId={taskId} />
            )}
          </>
        )}
      </div>
    </article>
  )
}
export const CanvasNodeView = memo(CanvasNodeCard)
