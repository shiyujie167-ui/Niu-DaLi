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
import { useQuery } from '@tanstack/react-query'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'

import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Field, FieldLabel } from '@/components/ui/field'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { TaskArtifactCard } from '@/features/usage-logs/components/task-artifacts'

import { getVideoArtifacts, getVideoMedia } from '../api'
import { useCanvasActions } from './context'
import type { CanvasNodeData } from './types'

export function CanvasResults(props: {
  id: string
  taskId: string
  data: CanvasNodeData
}) {
  const { t } = useTranslation()
  const actions = useCanvasActions()
  const query = useQuery({
    queryKey: ['video-workspace', 'task-artifacts', props.taskId],
    queryFn: () => getVideoArtifacts(props.taskId),
    retry: false,
    staleTime: 30_000,
    meta: { errorToast: false },
  })
  const artifacts =
    query.data?.artifacts.filter((artifact) => artifact.type === 'video') ?? []
  if (artifacts.length === 0 && query.data?.legacyContentUrl) {
    artifacts.push({
      key: 'video',
      type: 'video',
      mime_type: 'video/mp4',
      content_url: query.data.legacyContentUrl,
    })
  }
  const selected =
    artifacts.find((artifact) => artifact.key === props.data.artifact_key) ??
    artifacts[0]
  const updateNode = actions.updateNode
  useEffect(() => {
    if (selected && selected.key !== props.data.artifact_key) {
      updateNode(props.id, { artifact_key: selected.key })
    }
  }, [selected, props.data.artifact_key, props.id, updateNode])
  if (query.isPending) return <LoadingState className='min-h-24' />
  if (query.isError) {
    return (
      <ErrorState
        className='min-h-24'
        title={t('Failed to load artifacts')}
        onRetry={() => void query.refetch()}
      />
    )
  }
  if (!selected) {
    return (
      <p className='text-muted-foreground text-xs'>
        {t('Preview unavailable')}
      </p>
    )
  }
  return (
    <div className='nodrag nopan nowheel space-y-2'>
      {artifacts.length > 1 && (
        <Field>
          <FieldLabel htmlFor={`${props.id}-artifact`}>
            {t('Artifacts')}
          </FieldLabel>
          <NativeSelect
            id={`${props.id}-artifact`}
            value={selected.key}
            onChange={(event) =>
              actions.updateNode(props.id, { artifact_key: event.target.value })
            }
          >
            {artifacts.map((artifact) => (
              <NativeSelectOption key={artifact.key} value={artifact.key}>
                {artifact.key}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </Field>
      )}
      <TaskArtifactCard artifact={selected} loadMedia={getVideoMedia} />
    </div>
  )
}
