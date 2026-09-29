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

import { STATUS_QUERY_KEY } from '@/lib/status-query'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { CTA, Hero } from '../components'

function renderStorefront(
  isAuthenticated: boolean,
  status?: Record<string, unknown>
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  if (status) {
    queryClient.setQueryData(STATUS_QUERY_KEY, status)
  }
  const rootRoute = createRootRoute({
    component: () => (
      <>
        <Hero isAuthenticated={isAuthenticated} />
        <CTA isAuthenticated={isAuthenticated} />
      </>
    ),
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
    renderStorefront(false)

    expect(
      await screen.findByRole('heading', {
        name: 'NiuDaliTokenAccessForEverySupportedModel',
      })
    ).toHaveClass('[overflow-wrap:anywhere]')
  })

  it('sends a guest to account creation', async () => {
    renderStorefront(false)

    expect(
      await screen.findByRole('button', { name: /Create account/i })
    ).toHaveAttribute('href', '/sign-up')
    expect(
      screen.getByRole('button', { name: /Get Started/i })
    ).toHaveAttribute('href', '/sign-up')
  })

  it('sends a guest to sign-in when registration is disabled', async () => {
    renderStorefront(false, { register_enabled: false })

    const signInActions = await screen.findAllByRole('button', {
      name: /Sign in/i,
    })
    expect(signInActions).toHaveLength(2)
    for (const action of signInActions) {
      expect(action).toHaveAttribute('href', '/sign-in')
    }
    expect(
      screen.queryByRole('button', { name: /Create account|Get Started/i })
    ).not.toBeInTheDocument()
  })

  it('sends an authenticated user to the wallet', async () => {
    renderStorefront(true, { register_enabled: false })

    const walletActions = await screen.findAllByRole('button', {
      name: /Open wallet/i,
    })
    expect(walletActions).toHaveLength(2)
    for (const action of walletActions) {
      expect(action).toHaveAttribute('href', '/wallet')
    }
  })
})
