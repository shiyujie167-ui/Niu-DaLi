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
import type { Connection, Edge } from '@xyflow/react'
import type { TFunction } from 'i18next'

import type { TaskLog } from '@/features/usage-logs/types'

import { getVideoParameterDefaults } from '../lib/parameters'
import type { VideoWorkspaceModel } from '../types'
import type {
  CanvasGraph,
  CanvasNode,
  CanvasNodeKind,
  CanvasSubmission,
  CanvasVideoRequest,
} from './types'

export const EMPTY_CANVAS: CanvasGraph = {
  schema_version: 1,
  nodes: [],
  edges: [],
  viewport: { x: 0, y: 0, zoom: 1 },
}

export function serializeCanvas(graph: CanvasGraph): CanvasGraph {
  return {
    schema_version: 1,
    nodes: graph.nodes.map((node) => ({
      id: node.id,
      type: node.type,
      position: node.position,
      data: node.data,
    })),
    edges: graph.edges.map((edge) => ({
      id: edge.id,
      source: edge.source,
      target: edge.target,
      ...(edge.sourceHandle ? { sourceHandle: edge.sourceHandle } : {}),
      ...(edge.targetHandle ? { targetHandle: edge.targetHandle } : {}),
    })),
    viewport: graph.viewport,
  }
}
export function newCanvasNode(
  kind: CanvasNodeKind,
  position: { x: number; y: number },
  model?: VideoWorkspaceModel
): CanvasNode {
  let data: CanvasNode['data'] = { prompt: '' }
  if (kind === 'image') data = { asset_id: '' }
  if (kind === 'generation') {
    data = {
      prompt: '',
      model: model?.id ?? '',
      count: 1,
      ...(model ? getVideoParameterDefaults(model) : {}),
    }
  }
  return { id: crypto.randomUUID(), type: kind, position, data, selected: true }
}
export function cloneCanvasSelection(graph: CanvasGraph): {
  nodes: CanvasNode[]
  edges: Edge[]
} {
  const selected = graph.nodes.filter((node) => node.selected)
  const ids = new Map<string, string>()
  return {
    nodes: selected.map((node) => {
      const id = crypto.randomUUID()
      ids.set(node.id, id)
      const data = { ...node.data }
      if (node.type === 'generation') {
        delete data.task_id
        delete data.artifact_key
      }
      return {
        ...node,
        id,
        position: { x: node.position.x + 48, y: node.position.y + 48 },
        data,
        selected: true,
      }
    }),
    edges: graph.edges.flatMap((edge) => {
      const source = ids.get(edge.source)
      const target = ids.get(edge.target)
      if (!source || !target) return []
      return [
        {
          ...edge,
          id: crypto.randomUUID(),
          source,
          target,
          selected: false,
        },
      ]
    }),
  }
}
export function validCanvasConnection(
  connection: Connection | Edge,
  graph: CanvasGraph
): boolean {
  if (
    !connection.source ||
    !connection.target ||
    connection.source === connection.target
  ) {
    return false
  }
  if (
    graph.nodes.find((node) => node.id === connection.target)?.type !==
    'generation'
  ) {
    return false
  }
  if (
    graph.edges.some(
      (edge) =>
        edge.source === connection.source && edge.target === connection.target
    )
  ) {
    return false
  }
  const visited = new Set<string>()
  const pending = [connection.target]
  for (let index = 0; index < pending.length; index++) {
    const id = pending[index]
    if (id === connection.source) return false
    if (visited.has(id)) continue
    visited.add(id)
    for (const edge of graph.edges) {
      if (edge.source === id) pending.push(edge.target)
    }
  }
  return graph.nodes.some((node) => node.id === connection.source)
}
export function latestCanvasSubmissions(
  submissions: CanvasSubmission[]
): Map<string, CanvasSubmission> {
  const latest = new Map<string, CanvasSubmission>()
  for (const submission of submissions) {
    const previous = latest.get(submission.node_id)
    if (!previous || submission.created_at > previous.created_at) {
      latest.set(submission.node_id, submission)
    }
  }
  return latest
}
export function nodeTaskId(
  node: CanvasNode,
  submissions: Map<string, CanvasSubmission>
): string | undefined {
  const submission = submissions.get(node.id)
  // A pending newer submission must never expose a previous result as its output.
  if (submission) {
    return submission.status === 'accepted' ? submission.task_id : undefined
  }
  return node.data.task_id
}
export function compileCanvasVideo(
  graph: CanvasGraph,
  nodeId: string,
  models: VideoWorkspaceModel[],
  tasks: Map<string, TaskLog>,
  submissions: Map<string, CanvasSubmission>,
  t: TFunction
): Omit<CanvasVideoRequest, 'submission_id'> {
  const target = graph.nodes.find((node) => node.id === nodeId)
  const model = models.find((item) => item.id === target?.data.model)
  if (!target || target.type !== 'generation' || !model) {
    throw new Error(t('Choose an available video model'))
  }
  const sources = graph.edges
    .filter((edge) => edge.target === nodeId)
    .map((edge) => graph.nodes.find((node) => node.id === edge.source))
  const prompts: string[] = []
  const images: string[] = []
  const videos: { task_id: string; artifact_key: string }[] = []
  for (const source of sources) {
    if (!source) throw new Error(t('A connected input is missing'))
    if (source.type === 'prompt' && source.data.prompt?.trim()) {
      prompts.push(source.data.prompt.trim())
    }
    if (source.type === 'image') {
      if (!source.data.asset_id) {
        throw new Error(t('Upload every connected image before generating'))
      }
      if (
        !source.data.mime_type ||
        !model.supported_image_types?.includes(source.data.mime_type)
      ) {
        throw new Error(t('Choose a supported image file'))
      }
      if (
        typeof source.data.size !== 'number' ||
        source.data.size <= 0 ||
        source.data.size > (model.max_image_bytes ?? 0)
      ) {
        throw new Error(t('Image exceeds this model’s upload limit'))
      }
      images.push(source.data.asset_id)
    }
    if (source.type === 'generation') {
      const taskId = nodeTaskId(source, submissions)
      if (
        !taskId ||
        tasks.get(taskId)?.status !== 'SUCCESS' ||
        !source.data.artifact_key
      ) {
        throw new Error(
          t('Wait for connected videos to finish and select a result')
        )
      }
      videos.push({ task_id: taskId, artifact_key: source.data.artifact_key })
    }
  }
  if (target.data.prompt?.trim()) prompts.push(target.data.prompt.trim())
  const prompt = prompts.join('\n\n')
  if (!prompt) throw new Error(t('Enter a video prompt'))
  if ([...prompt].length > model.max_prompt_length) {
    throw new Error(t('Video prompt is too long'))
  }
  if (
    images.length >
    (model.max_reference_images ?? (model.supports_image ? 1 : 0))
  ) {
    throw new Error(t('This model does not support this many reference images'))
  }
  if (
    videos.length > (model.max_reference_videos ?? 0) ||
    (videos.length > 0 && !model.supports_video)
  ) {
    throw new Error(t('This model does not support these video references'))
  }
  if (images.length > 0 && videos.length > 0 && !model.supports_mixed_media) {
    throw new Error(t('This model cannot combine image and video references'))
  }
  const count = target.data.count ?? 1
  if (
    !Number.isInteger(count) ||
    count < 1 ||
    count > (model.max_outputs ?? 1)
  ) {
    throw new Error(t('Choose a supported output quantity'))
  }
  const seconds = target.data.seconds || undefined
  const size =
    typeof target.data.size === 'string'
      ? target.data.size || undefined
      : undefined
  const resolution = target.data.resolution || undefined
  if (seconds && !model.durations?.some((value) => String(value) === seconds)) {
    throw new Error(t('Choose a supported duration'))
  }
  if (size && !model.sizes?.includes(size)) {
    throw new Error(t('Choose a supported video size'))
  }
  if (resolution && !model.resolutions?.includes(resolution)) {
    throw new Error(t('Choose a supported video resolution'))
  }
  return {
    model: model.id,
    prompt,
    seconds,
    size,
    resolution,
    n: count,
    image_asset_ids: images,
    video_references: videos,
    canvas_node_id: nodeId,
  }
}
