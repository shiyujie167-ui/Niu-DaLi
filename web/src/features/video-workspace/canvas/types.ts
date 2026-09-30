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
import type { Edge, Node, Viewport } from '@xyflow/react'

import type { TaskLog } from '@/features/usage-logs/types'

import type { VideoWorkspaceCatalog } from '../types'

export type CanvasNodeKind = 'prompt' | 'image' | 'generation'
export interface CanvasNodeData extends Record<string, unknown> {
  prompt?: string
  asset_id?: string
  filename?: string
  mime_type?: string
  size?: string | number
  width?: number
  height?: number
  content_url?: string
  model?: string
  seconds?: string
  resolution?: string
  count?: number
  task_id?: string
  artifact_key?: string
}
export type CanvasNode = Node<CanvasNodeData, CanvasNodeKind>
export interface CanvasGraph {
  schema_version: 1
  nodes: CanvasNode[]
  edges: Edge[]
  viewport: Viewport
}
export interface CanvasSubmission {
  node_id: string
  submission_id: string
  task_id: string
  status: 'submitting' | 'accepted' | 'failed' | 'unknown'
  created_at: number
  updated_at: number
}
export interface CanvasDocument {
  revision: number
  graph: CanvasGraph
  submissions: CanvasSubmission[]
}
export interface CanvasAsset {
  id: string
  filename: string
  mime_type: string
  size: number
  width: number
  height: number
  content_url: string
}
export interface CanvasVideoRequest {
  model: string
  prompt: string
  seconds?: string
  size?: string
  resolution?: string
  n: number
  image_asset_ids: string[]
  video_references: { task_id: string; artifact_key: string }[]
  canvas_node_id: string
  submission_id: string
}
export interface CanvasActions {
  catalog: VideoWorkspaceCatalog
  graph: CanvasGraph
  tasks: Map<string, TaskLog>
  taskErrors: Map<string, unknown>
  errors: Map<string, unknown>
  submissions: Map<string, CanvasSubmission>
  busyNodes: Set<string>
  locked: boolean
  updateNode: (id: string, data: Partial<CanvasNodeData>) => void
  uploadImage: (id: string, file: File) => Promise<void>
  generate: (id: string) => Promise<void>
  deleteNode: (id: string) => void
  refresh: () => void
}
