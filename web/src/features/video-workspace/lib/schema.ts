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
import type { TFunction } from 'i18next'
import { z } from 'zod'

import type { VideoWorkspaceModel } from '../types'

export function videoFormSchema(model: VideoWorkspaceModel, t: TFunction) {
  return z.object({
    prompt: z
      .string()
      .trim()
      .min(1, t('Enter a video prompt'))
      .max(model.max_prompt_length || 4000, t('Video prompt is too long')),
    seconds: z
      .string()
      .refine(
        (value) =>
          !value ||
          model.durations?.some((seconds) => String(seconds) === value),
        t('Choose a supported duration')
      ),
    size: z
      .string()
      .refine(
        (value) => !value || model.sizes?.includes(value),
        t('Choose a supported video size')
      ),
    resolution: z
      .string()
      .refine(
        (value) => !value || model.resolutions?.includes(value),
        t('Choose a supported video resolution')
      ),
    image: z
      .instanceof(File)
      .optional()
      .superRefine((file, context) => {
        if (!file) return
        if (
          !model.supports_image ||
          !model.supported_image_types?.includes(file.type)
        ) {
          context.addIssue({
            code: 'custom',
            message: t('Choose a supported image file'),
          })
        }
        if (
          file.size === 0 ||
          !model.max_image_bytes ||
          file.size > model.max_image_bytes
        ) {
          context.addIssue({
            code: 'custom',
            message: t('Image exceeds this model’s upload limit'),
          })
        }
      }),
  })
}

export type VideoFormValues = z.infer<ReturnType<typeof videoFormSchema>>
