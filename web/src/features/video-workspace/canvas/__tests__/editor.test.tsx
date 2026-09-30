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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type { VideoWorkspaceCatalog } from '../../types'
import VideoCanvas from '../index'
import type { CanvasDocument, CanvasGraph } from '../types'

const clients: QueryClient[] = []
const catalog: VideoWorkspaceCatalog = {
  quota: 500000,
  models: [
    {
      id: 'model',
      name: 'Model',
      supports_image: true,
      max_reference_images: 2,
      max_image_bytes: 1024,
      supported_image_types: ['image/png'],
      max_prompt_length: 4000,
      durations: [5],
      max_outputs: 1,
    },
  ],
}
let stored: CanvasDocument

beforeEach(() => {
  stored = {
    revision: 1,
    submissions: [],
    graph: {
      schema_version: 1,
      nodes: [],
      edges: [],
      viewport: { x: 20, y: 30, zoom: 0.75 },
    },
  }
  Object.defineProperty(document, 'elementFromPoint', {
    configurable: true,
    value: () => null,
  })
  vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(800)
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockReturnValue(600)
  vi.stubGlobal(
    'DOMMatrixReadOnly',
    class {
      m22 = 1
    }
  )
  vi.stubGlobal(
    'ResizeObserver',
    class {
      private callback: ResizeObserverCallback
      private targets = new Set<Element>()
      constructor(callback: ResizeObserverCallback) {
        this.callback = callback
      }
      observe(target: Element) {
        this.targets.add(target)
        queueMicrotask(() => {
          if (this.targets.has(target)) {
            this.callback(
              [
                {
                  target,
                  contentRect: target.getBoundingClientRect(),
                } as ResizeObserverEntry,
              ],
              this as unknown as ResizeObserver
            )
          }
        })
      }
      unobserve(target: Element) {
        this.targets.delete(target)
      }
      disconnect() {
        this.targets.clear()
      }
    }
  )
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/video-workspace/canvas') {
      return { data: { success: true, data: structuredClone(stored) } }
    }
    if (url.includes('/tasks/')) {
      return {
        data: {
          success: true,
          data: {
            task_id: 'task-1',
            status: 'IN_PROGRESS',
            platform: 'provider',
          },
        },
      }
    }
    throw new Error(`Unexpected GET ${url}`)
  })
  vi.spyOn(api, 'put').mockImplementation(async (_url, body) => {
    const input = body as { revision: number; graph: CanvasGraph }
    stored = {
      ...stored,
      graph: structuredClone(input.graph),
      revision: input.revision + 1,
    }
    return { data: { success: true, data: stored } }
  })
})
afterEach(() => {
  clients.forEach((client) => client.clear())
  clients.length = 0
  vi.unstubAllGlobals()
  Reflect.deleteProperty(document, 'elementFromPoint')
})

async function openCanvas() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  const result = render(
    <QueryClientProvider client={client}>
      <VideoCanvas catalog={catalog} />
    </QueryClientProvider>
  )
  await screen.findByRole('region', { name: 'Infinite canvas' })
  return result
}

describe('infinite canvas editing', () => {
  test('adding, copying and deleting nodes supports undo and redo with keyboard shortcuts', async () => {
    await openCanvas()
    const user = userEvent.setup()
    expect(
      screen.getByText('Start with a prompt, image or video node')
    ).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Prompt node' }))
    expect(
      await screen.findByRole('textbox', { name: 'Video prompt' })
    ).toBeVisible()
    await user.click(
      screen.getByRole('button', { name: 'Duplicate selected nodes' })
    )
    expect(
      screen.getAllByRole('textbox', { name: 'Video prompt' })
    ).toHaveLength(2)
    await user.click(screen.getByRole('button', { name: 'Delete selection' }))
    expect(
      screen.getAllByRole('textbox', { name: 'Video prompt' })
    ).toHaveLength(1)
    await user.click(screen.getByRole('button', { name: 'Undo' }))
    expect(
      screen.getAllByRole('textbox', { name: 'Video prompt' })
    ).toHaveLength(2)
    await user.click(screen.getByRole('button', { name: 'Redo' }))
    expect(
      screen.getAllByRole('textbox', { name: 'Video prompt' })
    ).toHaveLength(1)
    screen.getByRole('region', { name: 'Infinite canvas' }).focus()
    await user.keyboard(
      '{Control>}a{/Control}{Control>}c{/Control}{Control>}v{/Control}'
    )
    expect(
      screen.getAllByRole('textbox', { name: 'Video prompt' })
    ).toHaveLength(2)
    await user.keyboard('{Delete}')
    expect(
      screen.getAllByRole('textbox', { name: 'Video prompt' })
    ).toHaveLength(1)
  })

  test('deleting a focused node returns focus to the canvas so keyboard undo restores it', async () => {
    await openCanvas()
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Prompt node' }))
    const node = await screen.findByRole('article', { name: 'Prompt node' })
    await user.click(within(node).getByRole('button', { name: 'Delete node' }))
    expect(
      screen.queryByRole('article', { name: 'Prompt node' })
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('region', { name: 'Infinite canvas' })
    ).toHaveFocus()
    await user.keyboard('{Control>}z{/Control}')
    expect(
      await screen.findByRole('article', { name: 'Prompt node' })
    ).toBeVisible()
  })

  test('editing text does not trigger canvas delete or copy shortcuts and saved prompts restore after reopening', async () => {
    const view = await openCanvas()
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Prompt node' }))
    const input = await screen.findByRole('textbox', { name: 'Video prompt' })
    await user.type(input, 'Sunrise!{Backspace}')
    expect(input).toHaveValue('Sunrise')
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await screen.findByText('Saved to server')
    expect(stored.graph.nodes[0].data.prompt).toBe('Sunrise')
    expect(stored.graph.nodes[0]).not.toHaveProperty('selected')
    view.unmount()
    await openCanvas()
    expect(
      await screen.findByRole('textbox', { name: 'Video prompt' })
    ).toHaveValue('Sunrise')
  })

  test('selection and pan tools expose their active state and zoom controls change the saved viewport', async () => {
    await openCanvas()
    const user = userEvent.setup()
    expect(screen.getByRole('button', { name: 'Pan canvas' })).toHaveAttribute(
      'aria-pressed',
      'true'
    )
    await user.click(screen.getByRole('button', { name: 'Select nodes' }))
    expect(
      screen.getByRole('button', { name: 'Select nodes' })
    ).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'Pan canvas' })).toHaveAttribute(
      'aria-pressed',
      'false'
    )
    await user.click(screen.getByRole('button', { name: 'Zoom in' }))
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
    )
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await screen.findByText('Saved to server')
    expect(stored.graph.viewport.zoom).toBeGreaterThan(0.75)
  })

  test('a failed document load offers retry without opening an empty replacement canvas', async () => {
    vi.mocked(api.get).mockRejectedValueOnce(new Error('Server unavailable'))
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    clients.push(client)
    render(
      <QueryClientProvider client={client}>
        <VideoCanvas catalog={catalog} />
      </QueryClientProvider>
    )
    expect(await screen.findByText('Failed to load canvas')).toBeVisible()
    expect(
      screen.queryByRole('region', { name: 'Infinite canvas' })
    ).not.toBeInTheDocument()
    await userEvent
      .setup()
      .click(screen.getByRole('button', { name: /Retry/i }))
    expect(
      await screen.findByRole('region', { name: 'Infinite canvas' })
    ).toBeVisible()
    expect(api.put).not.toHaveBeenCalled()
  })
})

describe('canvas generation and recovery', () => {
  beforeEach(() => {
    stored.graph.nodes = [
      {
        id: 'prompt',
        type: 'prompt',
        position: { x: 0, y: 0 },
        data: { prompt: 'Sunrise' },
      },
      {
        id: 'image',
        type: 'image',
        position: { x: 0, y: 300 },
        data: { asset_id: 'asset-1', mime_type: 'image/png', size: 100 },
      },
      {
        id: 'generation',
        type: 'generation',
        position: { x: 400, y: 0 },
        data: { model: 'model', seconds: '5', count: 1 },
      },
    ]
    stored.graph.edges = [
      { id: 'prompt-edge', source: 'prompt', target: 'generation' },
      { id: 'image-edge', source: 'image', target: 'generation' },
    ]
  })
  test('connecting two handles persists a real input edge and undo removes that connection', async () => {
    stored.graph.edges = []
    await openCanvas()
    const user = userEvent.setup()
    const prompt = await screen.findByRole('article', { name: 'Prompt node' })
    const generation = screen.getByRole('article', { name: 'Video generation' })
    await user.click(within(prompt).getByLabelText('Connect output'))
    await user.click(within(generation).getByLabelText('Connect input'))
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
    )
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await screen.findByText('Saved to server')
    expect(stored.graph.edges).toEqual([
      expect.objectContaining({
        source: 'prompt',
        target: 'generation',
        sourceHandle: 'output',
        targetHandle: 'input',
      }),
    ])
    await user.click(screen.getByRole('button', { name: 'Undo' }))
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await screen.findByText('Saved to server')
    expect(stored.graph.edges).toEqual([])
  })

  test.each(['artifact', 'legacy'])(
    'completed %s results preview and download inside their node and are saved for reuse',
    async (kind) => {
      stored.graph.nodes = [
        {
          id: 'finished',
          type: 'generation',
          position: { x: 0, y: 0 },
          data: { model: 'model', task_id: 'task-1' },
        },
      ]
      stored.graph.edges = []
      const createObjectURL = vi.fn(() => 'blob:canvas-result')
      const revokeObjectURL = vi.fn()
      vi.stubGlobal(
        'URL',
        class extends URL {
          static createObjectURL = createObjectURL
          static revokeObjectURL = revokeObjectURL
        }
      )
      vi.mocked(api.get).mockImplementation(async (url) => {
        if (url.endsWith('/canvas')) {
          return { data: { success: true, data: stored } }
        }
        if (url.endsWith('/content')) {
          return { data: new Blob(['video'], { type: 'video/mp4' }) }
        }
        if (url.endsWith('/artifacts')) {
          return {
            data: {
              success: true,
              data: {
                task_id: 'task-1',
                artifacts:
                  kind === 'artifact'
                    ? [
                        {
                          key: 'video',
                          type: 'video',
                          mime_type: 'video/mp4',
                          content_url:
                            '/api/video-workspace/tasks/task-1/artifacts/video/content',
                        },
                      ]
                    : [],
                legacy_content_url:
                  kind === 'legacy'
                    ? '/api/video-workspace/tasks/task-1/artifacts/video/content'
                    : undefined,
              },
            },
          }
        }
        return {
          data: {
            success: true,
            data: {
              task_id: 'task-1',
              status: 'SUCCESS',
              platform: 'provider',
            },
          },
        }
      })
      const view = await openCanvas()
      const card = await screen.findByRole('article', {
        name: 'Video generation',
      })
      const download = await within(card).findByRole('button', {
        name: 'Download',
      })
      await waitFor(() =>
        expect(download).toHaveAttribute('href', 'blob:canvas-result')
      )
      expect(card.querySelector('video')).toHaveAttribute(
        'src',
        'blob:canvas-result'
      )
      expect(card.querySelector('video')).toHaveAttribute('controls')
      await userEvent
        .setup()
        .click(screen.getByRole('button', { name: 'Save' }))
      await screen.findByText('Saved to server')
      expect(stored.graph.nodes[0].data).toMatchObject({
        task_id: 'task-1',
        artifact_key: 'video',
      })
      view.unmount()
      expect(revokeObjectURL).toHaveBeenCalledWith('blob:canvas-result')
    }
  )

  test('connected materials reach the existing generation endpoint and an accepted task is never automatically resubmitted', async () => {
    const post = vi
      .spyOn(api, 'post')
      .mockImplementation(async (_url, body) => {
        const request = body as { submission_id: string }
        stored.submissions = [
          {
            node_id: 'generation',
            submission_id: request.submission_id,
            task_id: 'task-1',
            status: 'accepted',
            created_at: 1,
            updated_at: 1,
          },
        ]
        return {
          data: {
            id: 'task-1',
            object: 'video',
            status: 'queued',
            model: 'model',
          },
        }
      })
    const view = await openCanvas()
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Generate video' }))
    expect(await screen.findByText('task-1')).toBeVisible()
    expect(post).toHaveBeenCalledTimes(1)
    expect(post.mock.calls[0][0]).toBe('/api/video-workspace/tasks')
    expect(post.mock.calls[0][1]).toMatchObject({
      prompt: 'Sunrise',
      image_asset_ids: ['asset-1'],
      model: 'model',
      seconds: '5',
      n: 1,
      canvas_node_id: 'generation',
      submission_id: expect.any(String),
    })
    expect(post.mock.calls[0][2]).toMatchObject({
      singleUseAuthorization: true,
    })
    expect(
      screen.getByRole('button', { name: 'Generate video' })
    ).toBeDisabled()
    view.unmount()
    await openCanvas()
    expect(await screen.findByText('task-1')).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Generate video' })
    ).toBeDisabled()
    expect(post).toHaveBeenCalledTimes(1)
  })

  test('a conflicting submission recovers the other tab’s task despite a newer local clock', async () => {
    const post = vi.spyOn(api, 'post').mockImplementation(async () => {
      stored.submissions = [
        {
          node_id: 'generation',
          submission_id: 'other-tab-submission',
          task_id: 'task-1',
          status: 'accepted',
          created_at: 1,
          updated_at: 1,
        },
      ]
      throw Object.assign(new Error('Submission already in progress'), {
        isAxiosError: true,
        response: { status: 409 },
      })
    })
    await openCanvas()
    await userEvent
      .setup()
      .click(screen.getByRole('button', { name: 'Generate video' }))
    expect(await screen.findByText('task-1')).toBeVisible()
    expect(
      screen.queryByText(
        'Submission is being reconciled. It will not be sent again.'
      )
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Generate video' })
    ).toBeDisabled()
    expect(post).toHaveBeenCalledTimes(1)
  })

  test('unsupported quantity blocks generation before any chargeable request', async () => {
    const post = vi.spyOn(api, 'post')
    await openCanvas()
    const user = userEvent.setup()
    const count = screen.getByRole('spinbutton', { name: 'Quantity' })
    await user.clear(count)
    await user.type(count, '2')
    await user.click(screen.getByRole('button', { name: 'Generate video' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Choose a supported output quantity'
    )
    expect(post).not.toHaveBeenCalled()
  })

  test('a disconnected submit keeps its durable pending claim and refresh recovers the accepted task without replay', async () => {
    const post = vi
      .spyOn(api, 'post')
      .mockImplementation(async (_url, body) => {
        const request = body as { submission_id: string }
        stored.submissions = [
          {
            node_id: 'generation',
            submission_id: request.submission_id,
            task_id: '',
            status: 'unknown',
            created_at: 1,
            updated_at: 1,
          },
        ]
        throw new Error('Connection interrupted')
      })
    await openCanvas()
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: 'Generate video' }))
    expect(
      await screen.findByText(
        'Submission is being reconciled. It will not be sent again.'
      )
    ).toBeVisible()
    expect(
      screen.getByRole('button', { name: 'Generate video' })
    ).toBeDisabled()
    stored.submissions[0] = {
      ...stored.submissions[0],
      status: 'accepted',
      task_id: 'task-1',
    }
    await user.click(
      within(
        screen.getByRole('article', { name: 'Video generation' })
      ).getByRole('button', { name: 'Refresh' })
    )
    expect(await screen.findByText('task-1')).toBeVisible()
    expect(post).toHaveBeenCalledTimes(1)
  })
})
