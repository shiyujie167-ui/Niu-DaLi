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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { AxiosRequestConfig } from 'axios'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { TaskArtifactsCell } from '@/features/usage-logs/components/task-artifacts'
import type { TaskLog } from '@/features/usage-logs/types'
import { api } from '@/lib/api'

import { getVideoArtifacts, getVideoMedia } from '../api'

const clients: QueryClient[] = []
const createObjectURL = vi.fn<(blob: Blob) => string>()
const revokeObjectURL = vi.fn<(url: string) => void>()
const contentBase = '/api/video-workspace/tasks/task-saved/artifacts/'
const task: TaskLog = {
  id: 1,
  user_id: 1,
  channel_id: 1,
  group: 'default',
  task_id: 'task-saved',
  platform: 'sora',
  action: 'text_to_video',
  quota: 10000,
  submit_time: 1700000000,
  status: 'SUCCESS',
}

beforeEach(() => {
  createObjectURL.mockReturnValue('blob:workspace-preview')
  vi.stubGlobal(
    'URL',
    class extends URL {
      static createObjectURL = createObjectURL
      static revokeObjectURL = revokeObjectURL
    }
  )
})

afterEach(() => {
  for (const client of clients) client.clear()
  clients.length = 0
  vi.unstubAllGlobals()
})

function renderViewer(legacy = false) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  return render(
    <QueryClientProvider client={client}>
      <TaskArtifactsCell
        log={{ ...task, legacy_video_available: legacy }}
        loadArtifacts={getVideoArtifacts}
        loadMedia={getVideoMedia}
        artifactQueryKey='video-workspace'
      />
    </QueryClientProvider>
  )
}

function mockVideoApi(
  load: (url: string, config?: AxiosRequestConfig) => Promise<{ data: Blob }>,
  keys: string[] = ['video'],
  legacy = false
) {
  return vi.spyOn(api, 'get').mockImplementation(async (url, config) => {
    if (url.endsWith('/artifacts')) {
      return {
        data: {
          success: true,
          data: {
            artifacts: legacy
              ? []
              : keys.map((key) => ({
                  key,
                  type: 'video',
                  mime_type: 'video/mp4',
                  content_url: `${contentBase}${key}/content`,
                })),
            legacy_content_url: legacy
              ? `${contentBase}video/content`
              : undefined,
          },
        },
      }
    }
    return load(url, config)
  })
}

describe('private workspace video media', () => {
  test('opening a legacy video loads authenticated bytes and revokes its preview URL when closed', async () => {
    const get = mockVideoApi(
      async () => ({ data: new Blob(['video'], { type: 'video/mp4' }) }),
      ['video'],
      true
    )
    renderViewer(true)
    const user = userEvent.setup()
    await user.click(
      screen.getByRole('button', { name: 'Click to preview video' })
    )
    const download = await screen.findByRole('button', { name: 'Download' })
    expect(download).toHaveAttribute('href', 'blob:workspace-preview')
    expect(download).toHaveAttribute('download', 'video.mp4')
    expect(document.querySelector('video')).toHaveAttribute(
      'src',
      'blob:workspace-preview'
    )
    expect(document.querySelector('video')).toHaveAttribute('controls')
    const request = get.mock.calls.find(
      ([url]) => url === `${contentBase}video/content`
    )
    expect(request?.[1]).toMatchObject({
      responseType: 'blob',
      disableDuplicate: true,
      signal: expect.any(AbortSignal),
    })
    await user.keyboard('{Escape}')
    await waitFor(() =>
      expect(revokeObjectURL).toHaveBeenCalledWith('blob:workspace-preview')
    )
    expect(request?.[1]?.signal?.aborted).toBe(true)
  })

  test('a media failure shows retry and does not expose the authenticated content path as a public link', async () => {
    const load = vi
      .fn()
      .mockRejectedValueOnce(new Error('Task or artifact not found'))
      .mockResolvedValueOnce({
        data: new Blob(['video'], { type: 'video/mp4' }),
      })
    mockVideoApi(load)
    renderViewer()
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Artifacts' }))
    expect(
      await screen.findByText('Media preview failed. Please try again.')
    ).toBeVisible()
    const download = screen.getByRole('button', { name: 'Download' })
    expect(download).toHaveAttribute('aria-disabled', 'true')
    expect(download).not.toHaveAttribute('href')
    expect(document.querySelector('video')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Retry' }))
    await waitFor(() =>
      expect(document.querySelector('video')).toHaveAttribute(
        'src',
        'blob:workspace-preview'
      )
    )
    expect(screen.getByRole('button', { name: 'Download' })).toHaveAttribute(
      'href',
      'blob:workspace-preview'
    )
    expect(load).toHaveBeenCalledTimes(2)
  })

  test('switching artifacts retains only the selected video blob', async () => {
    createObjectURL
      .mockReturnValueOnce('blob:first')
      .mockReturnValueOnce('blob:second')
    const get = mockVideoApi(
      async () => ({ data: new Blob(['video'], { type: 'video/mp4' }) }),
      ['first', 'second']
    )
    renderViewer()
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Artifacts' }))
    await waitFor(() =>
      expect(document.querySelector('video')).toHaveAttribute(
        'src',
        'blob:first'
      )
    )
    expect(
      get.mock.calls
        .filter(([url]) => url.endsWith('/content'))
        .map(([url]) => url)
    ).toEqual([`${contentBase}first/content`])
    await user.selectOptions(
      screen.getByRole('combobox', { name: 'Artifacts' }),
      'second'
    )
    await waitFor(() =>
      expect(document.querySelector('video')).toHaveAttribute(
        'src',
        'blob:second'
      )
    )
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:first')
    expect(document.querySelectorAll('video')).toHaveLength(1)
  })

  test('closing a loading viewer aborts the request and ignores a late response', async () => {
    let resolveMedia: (response: { data: Blob }) => void = () => undefined
    const media = new Promise<{ data: Blob }>((resolve) => {
      resolveMedia = resolve
    })
    const get = mockVideoApi(() => media)
    renderViewer()
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Artifacts' }))
    expect(await screen.findByLabelText('Loading...')).toBeVisible()
    expect(document.querySelector('video')).not.toBeInTheDocument()
    await user.keyboard('{Escape}')
    const request = get.mock.calls.find(([url]) => url.endsWith('/content'))
    expect(request?.[1]?.signal?.aborted).toBe(true)
    await act(async () => {
      resolveMedia({ data: new Blob(['video'], { type: 'video/mp4' }) })
      await media
    })
    expect(createObjectURL).not.toHaveBeenCalled()
  })

  test.each([
    'https://other.example/video.mp4',
    '/api/video-workspace/tasks/another-user-task/artifacts/video/content',
    `${contentBase}video/content?access=secret`,
    `${contentBase}../content`,
  ])(
    'rejects artifact metadata outside the exact authenticated task endpoint: %s',
    async (contentUrl) => {
      vi.spyOn(api, 'get').mockResolvedValue({
        data: {
          success: true,
          data: {
            artifacts: [
              { key: 'video', type: 'video', content_url: contentUrl },
            ],
          },
        },
      })
      await expect(getVideoArtifacts('task-saved')).rejects.toThrow(
        'invalid_content_url'
      )
      expect(createObjectURL).not.toHaveBeenCalled()
    }
  )
})
