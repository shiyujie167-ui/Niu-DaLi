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
import type { TaskLog } from '@/features/usage-logs/types'

export interface VideoWorkspaceModel {
  id: string
  name: string
  supports_image: boolean
  durations?: number[]
  sizes?: string[]
  max_image_bytes?: number
  max_prompt_length: number
  supported_image_types?: string[]
}

export interface VideoWorkspaceCatalog {
  models: VideoWorkspaceModel[]
  quota: number
}

export interface VideoWorkspaceHistory {
  items: TaskLog[]
  total: number
  page: number
  page_size: number
}

export interface VideoTaskReceipt {
  id: string
  object: 'video'
  model: string
  status: string
}

export interface VideoSubmission {
  model: string
  prompt: string
  seconds?: string
  size?: string
  image?: File
}
