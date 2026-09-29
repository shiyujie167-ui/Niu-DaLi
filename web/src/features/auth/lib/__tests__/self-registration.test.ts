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
import { describe, expect, it } from 'vitest'

import { isSelfRegistrationOpen } from '../self-registration'

describe('isSelfRegistrationOpen', () => {
  it.each([
    ['registration is enabled', { register_enabled: true }, true],
    ['registration is disabled', { register_enabled: false }, false],
    [
      'self-use mode is enabled',
      { register_enabled: true, self_use_mode_enabled: true },
      false,
    ],
    ['the flag is missing from status', {}, true],
    ['status is not loaded', null, true],
  ])('returns the expected value when %s', (_case, status, expected) => {
    expect(isSelfRegistrationOpen(status)).toBe(expected)
  })
})
