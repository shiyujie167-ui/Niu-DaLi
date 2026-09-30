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
import { fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { PublicHeader } from '../components/public-header'

vi.mock('@/hooks/use-notifications', () => ({
  useNotifications: () => ({
    activeTab: 'notice',
    announcements: [],
    loading: false,
    notice: '',
    popoverOpen: false,
    setActiveTab: vi.fn(),
    setPopoverOpen: vi.fn(),
    unreadCount: 0,
  }),
}))

vi.mock('@/hooks/use-top-nav-links', () => ({
  useTopNavLinks: () => [],
}))

function renderHeader() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const rootRoute = createRootRoute({
    component: () => (
      <PublicHeader
        className='niu-dali-header-theme'
        heroSurfaceUntilScrolled
        navLinks={[]}
        showAuthButtons={false}
        showLanguageSwitcher={false}
        showNotifications={false}
        showThemeSwitch={false}
      />
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
  useAuthStore.setState(useAuthStore.getInitialState(), true)
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  useSystemConfigStore.getState().setLoading(false)
  Object.defineProperty(window, 'scrollY', {
    configurable: true,
    value: 0,
    writable: true,
  })
})

describe('PublicHeader hero surface', () => {
  it.each(['NEW API', 'NEW APIY'])(
    'shows Niu Dali in the navigation when the cached site name is %s',
    async (systemName) => {
      useSystemConfigStore.getState().setConfig({ systemName })
      renderHeader()

      const header = await screen.findByRole('banner')
      expect(within(header).getByTitle('Niu Dali')).toBeVisible()
    }
  )

  it('uses the hero surface at the top of the page', async () => {
    renderHeader()

    const header = await screen.findByRole('banner')
    const navigation = within(header).getByRole('navigation')
    expect(header.parentElement).toHaveClass('niu-dali-header-theme')
    expect(navigation).toHaveClass('public-header-hero-surface')
  })

  it('returns to the normal surface after the page is scrolled', async () => {
    renderHeader()

    const header = await screen.findByRole('banner')
    const navigation = within(header).getByRole('navigation')

    Object.defineProperty(window, 'scrollY', {
      configurable: true,
      value: 21,
    })
    fireEvent.scroll(window)

    expect(navigation).not.toHaveClass('public-header-hero-surface')
  })

  it('returns to the normal surface while the mobile menu is open', async () => {
    const user = userEvent.setup()
    renderHeader()

    const header = await screen.findByRole('banner')
    const navigation = within(header).getByRole('navigation')
    const toggle = within(header).getByRole('button', {
      name: 'Toggle navigation menu',
    })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(navigation).toHaveClass('public-header-hero-surface')

    await user.click(toggle)

    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(navigation).not.toHaveClass('public-header-hero-surface')
  })

  it('keeps the closed mobile navigation out of keyboard focus order', async () => {
    const user = userEvent.setup()
    renderHeader()

    const header = await screen.findByRole('banner')
    const toggle = within(header).getByRole('button', {
      name: 'Toggle navigation menu',
    })
    const mobileNavigation = document.querySelector<HTMLElement>(
      '#public-mobile-navigation'
    )
    expect(mobileNavigation).toHaveAttribute('inert')
    expect(mobileNavigation).toHaveAttribute('aria-hidden', 'true')

    toggle.focus()
    await user.tab()

    expect(mobileNavigation).not.toContainElement(
      document.activeElement as HTMLElement | null
    )
  })
})
