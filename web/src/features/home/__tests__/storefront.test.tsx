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
import { render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'

import { useSystemConfigStore } from '@/stores/system-config-store'

import { Hero } from '../components'

function renderHero(isAuthenticated: boolean) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const rootRoute = createRootRoute({
    component: () => <Hero isAuthenticated={isAuthenticated} />,
  })
  const router = createRouter({
    routeTree: rootRoute,
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })

  return render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  useSystemConfigStore.getState().setConfig({
    systemName: '大力牛',
    logo: '/niu-dali-icon.png',
  })
})

describe('Niu Dali storefront primary action', () => {
  it('allows long system names to wrap inside the hero', async () => {
    useSystemConfigStore.getState().setConfig({
      systemName: 'NiuDaliTokenAccessForEverySupportedModel',
      logo: '/niu-dali-icon.png',
    })
    renderHero(false)

    expect(
      await screen.findByRole('heading', {
        name: 'NiuDaliTokenAccessForEverySupportedModel',
      })
    ).toHaveClass('[overflow-wrap:anywhere]')
  })

  it('sends a guest to account creation', async () => {
    renderHero(false)

    expect(
      await screen.findByRole('button', { name: /Create account/i })
    ).toHaveAttribute('href', '/sign-up')
  })

  it('sends an authenticated user to the wallet', async () => {
    renderHero(true)

    expect(
      await screen.findByRole('button', { name: /Open wallet/i })
    ).toHaveAttribute('href', '/wallet')
  })
})
