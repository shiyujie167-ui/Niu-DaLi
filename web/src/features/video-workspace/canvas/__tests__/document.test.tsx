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
import { act, renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type { CanvasDocument, CanvasGraph } from '../types'
import { useCanvasDocument } from '../use-canvas-document'

const clients: QueryClient[] = []
const initial: CanvasDocument = {
  revision: 1,
  graph: {
    schema_version: 1,
    nodes: [
      {
        id: 'prompt-1',
        type: 'prompt',
        position: { x: 20, y: 40 },
        data: { prompt: 'Sunrise' },
      },
    ],
    edges: [],
    viewport: { x: 0, y: 0, zoom: 1 },
  },
  submissions: [],
}

function openDocument() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(client)
  return renderHook(() => useCanvasDocument(initial), {
    wrapper: (props: { children: ReactNode }) => (
      <QueryClientProvider client={client}>
        {props.children}
      </QueryClientProvider>
    ),
  })
}

beforeEach(() => vi.useFakeTimers())
afterEach(() => {
  for (const client of clients) client.clear()
  clients.length = 0
  vi.useRealTimers()
})

describe('canvas document editing and persistence', () => {
  test('editing a prompt supports undo and redo without saving transient selection state', () => {
    const { result } = openDocument()
    act(() =>
      result.current.update((graph) => ({
        ...graph,
        nodes: graph.nodes.map((node) => ({
          ...node,
          data: { prompt: 'Moonrise' },
        })),
      }))
    )
    expect(result.current.graph.nodes[0].data.prompt).toBe('Moonrise')
    expect(result.current.canUndo).toBe(true)
    expect(result.current.saved).toBe(false)

    act(() => result.current.undo())
    expect(result.current.graph.nodes[0].data.prompt).toBe('Sunrise')
    expect(result.current.saved).toBe(true)
    expect(result.current.canRedo).toBe(true)

    act(() => result.current.redo())
    expect(result.current.graph.nodes[0].data.prompt).toBe('Moonrise')
    expect(result.current.saved).toBe(false)
    act(() =>
      result.current.update(
        (graph) => ({
          ...graph,
          nodes: graph.nodes.map((node) => ({ ...node, selected: true })),
        }),
        false
      )
    )
    act(() => result.current.undo())
    expect(result.current.graph.nodes[0].data.prompt).toBe('Sunrise')
  })

  test('overlapping save requests serialize revisions and persist the newest edit exactly once', async () => {
    let finishFirst!: () => void
    let finishSecond!: () => void
    const firstResponse = new Promise<void>((resolve) => {
      finishFirst = resolve
    })
    const secondResponse = new Promise<void>((resolve) => {
      finishSecond = resolve
    })
    const put = vi.spyOn(api, 'put').mockImplementation(async (_url, body) => {
      const input = body as { revision: number; graph: CanvasGraph }
      await (input.revision === 1 ? firstResponse : secondResponse)
      return {
        data: {
          success: true,
          data: {
            revision: input.revision + 1,
            graph: input.graph,
            submissions: [],
          },
        },
      }
    })
    const { result } = openDocument()
    act(() =>
      result.current.update((graph) => ({
        ...graph,
        nodes: graph.nodes.map((node) => ({
          ...node,
          data: { prompt: 'First edit' },
          selected: true,
        })),
      }))
    )
    let firstSave!: Promise<void>
    await act(async () => {
      firstSave = result.current.save()
      await Promise.resolve()
    })
    expect(put).toHaveBeenCalledTimes(1)

    act(() =>
      result.current.update((graph) => ({
        ...graph,
        nodes: graph.nodes.map((node) => ({
          ...node,
          data: { prompt: 'Newest edit' },
        })),
      }))
    )
    let secondSave!: Promise<void>
    let concurrentSave!: Promise<void>
    await act(async () => {
      secondSave = result.current.save()
      concurrentSave = result.current.save()
      finishFirst()
      await firstSave
    })
    expect(put).toHaveBeenCalledTimes(2)
    expect(put.mock.calls[0][1]).toMatchObject({
      revision: 1,
      graph: { nodes: [{ data: { prompt: 'First edit' } }] },
    })
    expect(put.mock.calls[1][1]).toMatchObject({
      revision: 2,
      graph: { nodes: [{ data: { prompt: 'Newest edit' } }] },
    })
    const persisted = put.mock.calls[1][1] as { graph: CanvasGraph }
    expect(persisted.graph.nodes[0]).not.toHaveProperty('selected')
    expect(result.current.saved).toBe(false)

    await act(async () => {
      finishSecond()
      await Promise.all([secondSave, concurrentSave])
    })
    expect(put).toHaveBeenCalledTimes(2)
    expect(result.current.saved).toBe(true)
    expect(result.current.conflict).toBe(false)
  })

  test('a revision conflict preserves local edits and blocks writes until the server document is restored', async () => {
    const conflict = Object.assign(
      new Error('Canvas changed in another session'),
      { isAxiosError: true, response: { status: 409 } }
    )
    const put = vi.spyOn(api, 'put').mockRejectedValueOnce(conflict)
    const { result } = openDocument()
    act(() =>
      result.current.update((graph) => ({
        ...graph,
        nodes: graph.nodes.map((node) => ({
          ...node,
          data: { prompt: 'Unsaved local edit' },
        })),
      }))
    )
    await act(async () => {
      await expect(result.current.save()).rejects.toBe(conflict)
    })
    expect(result.current.conflict).toBe(true)
    expect(result.current.saveError).toBe(conflict)
    expect(result.current.graph.nodes[0].data.prompt).toBe('Unsaved local edit')
    expect(result.current.saved).toBe(false)
    await act(async () => {
      await expect(result.current.save()).rejects.toThrow(
        'Canvas revision conflict'
      )
    })
    expect(put).toHaveBeenCalledTimes(1)

    const remote: CanvasDocument = {
      ...initial,
      revision: 7,
      graph: {
        ...initial.graph,
        nodes: [{ ...initial.graph.nodes[0], data: { prompt: 'Server edit' } }],
      },
    }
    act(() => result.current.restore(remote))
    expect(result.current.conflict).toBe(false)
    expect(result.current.saveError).toBeUndefined()
    expect(result.current.graph.nodes[0].data.prompt).toBe('Server edit')
    expect(result.current.saved).toBe(true)
    expect(result.current.canUndo).toBe(false)
    put.mockImplementation(async (_url, body) => {
      const input = body as { revision: number; graph: CanvasGraph }
      return {
        data: {
          success: true,
          data: { ...remote, revision: 8, graph: input.graph },
        },
      }
    })
    act(() =>
      result.current.update((graph) => ({
        ...graph,
        viewport: { x: 100, y: 20, zoom: 0.8 },
      }))
    )
    await act(async () => {
      await result.current.save()
    })
    expect(put.mock.calls[1][1]).toMatchObject({
      revision: 7,
      graph: { viewport: { x: 100, y: 20, zoom: 0.8 } },
    })
    expect(result.current.saved).toBe(true)
  })

  test('a failed save retains unsaved edits and succeeds only after an explicit retry', async () => {
    const failure = new Error('Offline')
    const put = vi
      .spyOn(api, 'put')
      .mockRejectedValueOnce(failure)
      .mockImplementationOnce(async (_url, body) => {
        const input = body as { revision: number; graph: CanvasGraph }
        return {
          data: {
            success: true,
            data: { revision: 2, graph: input.graph, submissions: [] },
          },
        }
      })
    const { result } = openDocument()
    act(() =>
      result.current.update((graph) => ({
        ...graph,
        viewport: { x: 50, y: 10, zoom: 1.2 },
      }))
    )
    await act(async () => {
      await expect(result.current.save()).rejects.toBe(failure)
    })
    expect(result.current.saveError).toBe(failure)
    expect(result.current.saved).toBe(false)
    expect(result.current.conflict).toBe(false)
    expect(result.current.graph.viewport).toEqual({ x: 50, y: 10, zoom: 1.2 })
    await act(async () => {
      await result.current.save()
    })
    expect(result.current.saved).toBe(true)
    expect(result.current.saveError).toBeUndefined()
    expect(put).toHaveBeenCalledTimes(2)
  })
})
