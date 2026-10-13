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
import { cleanup, renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import { useTopNavLinks } from '../use-top-nav-links'

let client: QueryClient

beforeEach(() => {
  vi.stubGlobal('localStorage', {
    getItem: () => null,
    setItem: () => undefined,
    removeItem: () => undefined,
  })
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  useAuthStore.getState().auth.reset()
})

afterEach(() => {
  cleanup()
  client.clear()
  useAuthStore.getState().auth.reset()
  vi.unstubAllGlobals()
})

function topNavFor(canvasLink?: string) {
  client.setQueryData(['status'], { canvas_link: canvasLink })
  function Wrapper(props: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        {props.children}
      </QueryClientProvider>
    )
  }
  return renderHook(() => useTopNavLinks(), { wrapper: Wrapper })
}

describe('external canvas top navigation', () => {
  it('a configured canvas URL exposes the external workspace and trims surrounding whitespace', () => {
    const { result } = topNavFor(
      '  https://canvas.example.test/video?source=gateway  '
    )
    expect(
      result.current.find((link) => link.title === 'Infinite canvas')
    ).toEqual({
      title: 'Infinite canvas',
      href: 'https://canvas.example.test/video?source=gateway',
      external: true,
    })
  })

  it('opens the canvas in a new tab so the current page is left untouched', () => {
    const { result } = topNavFor('https://canvas.example.test/')
    expect(
      result.current.find((link) => link.title === 'Infinite canvas')
        ?.external
    ).toBe(true)
  })

  it.each([undefined, '', '   '])(
    'an unconfigured canvas URL hides its navigation entry (%j)',
    (canvasLink) => {
      const { result } = topNavFor(canvasLink)
      expect(
        result.current.some((link) => link.title === 'Infinite canvas')
      ).toBe(false)
    }
  )
})
