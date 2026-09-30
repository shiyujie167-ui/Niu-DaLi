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
  parseTaskArtifactsResponse,
  TaskArtifactApiError,
} from '@/features/usage-logs/lib/task-artifacts'
import type { TaskArtifactsResponse } from '@/features/usage-logs/types'
import { api } from '@/lib/api'
import {
  createServerError,
  requireServerSuccess,
} from '@/lib/server-error-message'

import type {
  VideoSubmission,
  VideoTaskReceipt,
  VideoWorkspaceCatalog,
  VideoWorkspaceHistory,
} from './types'

interface ApiResponse<T> {
  success: boolean
  message?: string
  data: T
}

export async function getVideoModels(): Promise<VideoWorkspaceCatalog> {
  const response = await api.get<ApiResponse<VideoWorkspaceCatalog>>(
    '/api/video-workspace/models'
  )
  return requireServerSuccess(response.data).data
}

export async function getVideoHistory(
  page: number,
  pageSize: number
): Promise<VideoWorkspaceHistory> {
  const response = await api.get<ApiResponse<VideoWorkspaceHistory>>(
    '/api/video-workspace/tasks',
    {
      params: { p: page, page_size: pageSize },
    }
  )
  return requireServerSuccess(response.data).data
}

export async function createVideoTask(
  submission: VideoSubmission
): Promise<VideoTaskReceipt> {
  const data = new FormData()
  data.set('model', submission.model)
  data.set('prompt', submission.prompt)
  if (submission.seconds) data.set('seconds', submission.seconds)
  if (submission.size) data.set('size', submission.size)
  if (submission.resolution) data.set('resolution', submission.resolution)
  if (submission.image) data.set('input_reference', submission.image)
  const response = await api.post<VideoTaskReceipt>(
    '/api/video-workspace/tasks',
    data,
    {
      singleUseAuthorization: true,
      skipErrorHandler: true,
      headers: { 'Content-Type': undefined },
    }
  )
  const receipt = requireServerSuccess(response.data)
  if (!receipt.id || receipt.object !== 'video') {
    throw createServerError(response.data, 'Failed to submit video task')
  }
  return receipt
}

export async function getVideoArtifacts(taskId: string) {
  const response = await api.get<TaskArtifactsResponse>(
    `/api/video-workspace/tasks/${encodeURIComponent(taskId)}/artifacts`
  )
  const prefix = `/api/video-workspace/tasks/${encodeURIComponent(taskId)}/artifacts/`
  return parseTaskArtifactsResponse(response.data, (value) => {
    if (
      typeof value !== 'string' ||
      !value.startsWith(prefix) ||
      !/^[A-Za-z0-9][A-Za-z0-9._~-]{0,127}\/content$/.test(
        value.slice(prefix.length)
      )
    ) {
      throw new TaskArtifactApiError('invalid_content_url')
    }
    return value
  })
}

export async function getVideoMedia(
  contentUrl: string,
  signal: AbortSignal
): Promise<Blob> {
  if (
    !/^\/api\/video-workspace\/tasks\/[A-Za-z0-9_-]+\/artifacts\/[A-Za-z0-9][A-Za-z0-9._~-]{0,127}\/content$/.test(
      contentUrl
    )
  ) {
    throw new TaskArtifactApiError('invalid_content_url')
  }
  const response = await api.get<Blob>(contentUrl, {
    responseType: 'blob',
    signal,
    disableDuplicate: true,
    skipErrorHandler: true,
  })
  return response.data
}
