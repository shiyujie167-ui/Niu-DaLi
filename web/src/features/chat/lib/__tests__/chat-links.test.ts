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
import { afterEach, describe, expect, it } from 'vitest'

import { useSystemConfigStore } from '@/stores/system-config-store'

import { resolveChatUrl } from '../chat-links'

afterEach(() => {
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
})

describe('resolveChatUrl', () => {
  it('names the AQBot provider after the configured site when the template imports into AQBot', () => {
    useSystemConfigStore.getState().setConfig({ systemName: '大力牛' })

    const url = resolveChatUrl({
      template: 'aqbot://providers/import?{aqbotConfig}',
      apiKey: 'abc',
      serverAddress: 'https://api.example.com',
    })

    const query = new URLSearchParams(url.split('?')[1])
    expect(query.get('name')).toBe('大力牛')
    expect(query.get('baseurl')).toBe('https://api.example.com')
    expect(query.get('apikey')).toBe('sk-abc')
  })
})
