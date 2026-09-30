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
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { VideoWorkspace } from '../index'
import type { VideoWorkspaceCatalog } from '../types'

const clients: QueryClient[] = []
const catalog: VideoWorkspaceCatalog = {
  quota: 500000,
  models: [
    {
      id: 'sora-2',
      name: 'Sora 2',
      supports_image: false,
      durations: [4],
      max_prompt_length: 4000,
    },
  ],
}

async function openWorkspace() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  const root = createRootRoute({ component: VideoWorkspace })
  const router = createRouter({
    routeTree: root,
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  await router.load()
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

afterEach(() => {
  for (const client of clients) client.clear()
  clients.length = 0
})

describe('video workspace server-backed state', () => {
  test('switching to the canvas keeps the ordinary form draft and does not submit a task', async () => {
    vi.spyOn(api, 'get').mockImplementation(async (url) => {
      let data: unknown = { items: [], total: 0 }
      if (url.endsWith('/models')) data = catalog
      if (url.endsWith('/canvas')) {
        data = {
          revision: 0,
          submissions: [],
          graph: {
            schema_version: 1,
            nodes: [],
            edges: [],
            viewport: { x: 0, y: 0, zoom: 1 },
          },
        }
      }
      return { data: { success: true, data } }
    })
    const post = vi.spyOn(api, 'post')
    await openWorkspace()
    const user = userEvent.setup()
    await user.type(
      await screen.findByRole('textbox', { name: 'Video prompt' }),
      'Keep my normal video draft'
    )
    await user.click(screen.getByRole('tab', { name: 'Infinite canvas' }))
    expect(
      await screen.findByRole('region', { name: 'Infinite canvas' })
    ).toBeVisible()
    expect(
      screen.getByRole('tab', { name: 'Infinite canvas' })
    ).toHaveAttribute('aria-selected', 'true')
    expect(
      screen.queryByRole('textbox', { name: 'Video prompt' })
    ).not.toBeInTheDocument()
    await user.click(screen.getByRole('tab', { name: 'Standard' }))
    expect(screen.getByRole('textbox', { name: 'Video prompt' })).toHaveValue(
      'Keep my normal video draft'
    )
    expect(
      screen.queryByRole('region', { name: 'Infinite canvas' })
    ).not.toBeInTheDocument()
    expect(post).not.toHaveBeenCalled()
  })

  test('without a configured channel, explains the configuration gap and offers no generation controls', async () => {
    vi.spyOn(api, 'get').mockImplementation(async (url) => ({
      data: {
        success: true,
        data: url.endsWith('/models')
          ? { models: [], quota: 500000 }
          : { items: [], total: 0 },
      },
    }))
    const post = vi.spyOn(api, 'post')
    await openWorkspace()

    expect(await screen.findByText('No video channels available')).toBeVisible()
    expect(
      screen.getByText(
        'Ask your administrator to configure a supported video channel and model pricing for your group.'
      )
    ).toBeVisible()
    expect(
      screen.queryByRole('button', { name: 'Generate video' })
    ).not.toBeInTheDocument()
    expect(post).not.toHaveBeenCalled()
  })

  test('a persisted task remains visible after reopening and a refresh displays the server completion state', async () => {
    let status = 'IN_PROGRESS'
    vi.spyOn(api, 'get').mockImplementation(async (url) => ({
      data: {
        success: true,
        data: url.endsWith('/models')
          ? catalog
          : {
              items: [
                {
                  id: 1,
                  user_id: 1,
                  channel_id: 1,
                  group: 'default',
                  task_id: 'task-saved',
                  platform: 'openai',
                  platform_name: 'Video Provider',
                  action: 'GENERATE',
                  quota: 10000,
                  submit_time: 1700000000,
                  status,
                  properties: {
                    input: 'Saved sunrise prompt',
                  },
                },
              ],
              total: 1,
            },
      },
    }))
    const first = await openWorkspace()
    expect(await screen.findByText('Saved sunrise prompt')).toBeVisible()
    expect(screen.getByText('Video Provider')).toBeVisible()
    expect(screen.getByText('In Progress')).toBeVisible()
    first.unmount()
    await openWorkspace()
    expect(await screen.findByText('task-saved')).toBeVisible()
    status = 'SUCCESS'
    await userEvent
      .setup()
      .click(screen.getByRole('button', { name: 'Refresh' }))
    expect(await screen.findByText('Success')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Artifacts' })).toBeVisible()
  })

  test('a failed submit keeps the prompt, displays the backend reason, and never automatically retries the chargeable request', async () => {
    vi.spyOn(api, 'get').mockImplementation(async (url) => ({
      data: {
        success: true,
        data: url.endsWith('/models') ? catalog : { items: [], total: 0 },
      },
    }))
    const post = vi
      .spyOn(api, 'post')
      .mockRejectedValue(new Error('Video channel temporarily unavailable'))
    await openWorkspace()
    const user = userEvent.setup()
    await user.type(
      await screen.findByRole('textbox', { name: 'Video prompt' }),
      'My sunrise scene'
    )
    await user.click(screen.getByRole('button', { name: 'Generate video' }))

    expect(
      await screen.findByText('Video channel temporarily unavailable')
    ).toBeVisible()
    expect(screen.getByRole('textbox', { name: 'Video prompt' })).toHaveValue(
      'My sunrise scene'
    )
    expect(
      screen.getByText(
        'Check your video history before submitting again if the connection was interrupted.'
      )
    ).toBeVisible()
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'Generate video' })
      ).toBeEnabled()
    )
    expect(post).toHaveBeenCalledTimes(1)
    expect(post.mock.calls[0][2]).toMatchObject({
      singleUseAuthorization: true,
    })
    const payload = post.mock.calls[0][1] as FormData
    expect(payload.get('prompt')).toBe('My sunrise scene')
    expect(payload.get('model')).toBe('sora-2')
    expect(payload.get('seconds')).toBe('4')
    expect(payload.has('api_key')).toBe(false)
  })
})
