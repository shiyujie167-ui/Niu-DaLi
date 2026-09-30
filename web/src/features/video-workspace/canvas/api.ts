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
import type { TaskLog } from '@/features/usage-logs/types'
import { api } from '@/lib/api'
import {
  createServerError,
  requireServerSuccess,
} from '@/lib/server-error-message'

import type { VideoTaskReceipt } from '../types'
import type {
  CanvasAsset,
  CanvasDocument,
  CanvasGraph,
  CanvasVideoRequest,
} from './types'

interface Response<T> {
  success: boolean
  message?: string
  data: T
}
export async function getCanvas(): Promise<CanvasDocument> {
  const response = await api.get<Response<CanvasDocument>>(
    '/api/video-workspace/canvas',
    { disableDuplicate: true }
  )
  return requireServerSuccess(response.data).data
}
export async function saveCanvas(
  revision: number,
  graph: CanvasGraph
): Promise<CanvasDocument> {
  const response = await api.put<Response<CanvasDocument>>(
    '/api/video-workspace/canvas',
    { revision, graph }
  )
  return requireServerSuccess(response.data).data
}
export async function uploadCanvasAsset(file: File): Promise<CanvasAsset> {
  const form = new FormData()
  form.set('file', file)
  const response = await api.post<Response<CanvasAsset>>(
    '/api/video-workspace/assets',
    form,
    { headers: { 'Content-Type': undefined } }
  )
  return requireServerSuccess(response.data).data
}
export async function loadCanvasImage(
  contentUrl: string,
  signal: AbortSignal
): Promise<Blob> {
  if (
    !/^\/api\/video-workspace\/assets\/[A-Za-z0-9_-]+\/content$/.test(
      contentUrl
    )
  ) {
    throw new Error('Invalid canvas image')
  }
  const response = await api.get<Blob>(contentUrl, {
    signal,
    responseType: 'blob',
    disableDuplicate: true,
    skipErrorHandler: true,
  })
  return response.data
}
export async function getCanvasTask(taskId: string): Promise<TaskLog> {
  const response = await api.get<Response<TaskLog>>(
    `/api/video-workspace/tasks/${encodeURIComponent(taskId)}`
  )
  return requireServerSuccess(response.data).data
}
export async function createCanvasVideo(
  request: CanvasVideoRequest
): Promise<VideoTaskReceipt> {
  const response = await api.post<VideoTaskReceipt>(
    '/api/video-workspace/tasks',
    request,
    { singleUseAuthorization: true, skipErrorHandler: true }
  )
  const receipt = requireServerSuccess(response.data)
  if (!receipt.id || receipt.object !== 'video') {
    throw createServerError(response.data, 'Failed to submit video task')
  }
  return receipt
}
