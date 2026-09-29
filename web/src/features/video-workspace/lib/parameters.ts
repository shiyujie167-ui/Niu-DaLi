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
// Adapted from Open Generative AI videoModelParameters.js,
// commit 9f2ed4f0995c6f969ee1706454f2f8c703f6d4f5.
// Copyright (c) 2026 Open Generative AI Contributors (MIT).
// Full notice: /licenses/Open-Generative-AI.txt.
import type { VideoWorkspaceModel } from '../types'

export function matchingVideoParameterValue(
  options: readonly (string | number)[] | undefined,
  value: string | undefined
): string | undefined {
  if (!value) return undefined
  const match = options?.find((option) => String(option) === value)
  return match === undefined ? undefined : String(match)
}

export function getVideoParameterDefaults(model: VideoWorkspaceModel): {
  seconds: string
  size: string
} {
  return {
    seconds:
      model.durations?.[0] === undefined ? '' : String(model.durations[0]),
    size: model.sizes?.[0] ?? '',
  }
}
