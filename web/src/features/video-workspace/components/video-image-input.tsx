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
import { useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { FormControl, FormDescription } from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

interface VideoImageInputProps {
  file?: File
  onChange: (file: File | undefined) => void
  accept: string
  maxBytes: number
}

export function VideoImageInput(props: VideoImageInputProps) {
  const { t, i18n } = useTranslation()
  const inputRef = useRef<HTMLInputElement>(null)
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  return (
    <>
      <FormControl>
        <Input
          ref={inputRef}
          type='file'
          accept={props.accept}
          onChange={(event) => props.onChange(event.target.files?.[0])}
        />
      </FormControl>
      <FormDescription>
        {t('PNG, JPEG or WebP, up to {{size}} MB', {
          size: formatNumber(props.maxBytes / 1024 / 1024, locale),
        })}
      </FormDescription>
      {props.file && (
        <div className='flex min-w-0 items-center gap-2'>
          <span className='text-muted-foreground min-w-0 flex-1 truncate text-xs'>
            {props.file.name}
          </span>
          <Button
            type='button'
            variant='ghost'
            size='sm'
            onClick={() => {
              props.onChange(undefined)
              if (inputRef.current) inputRef.current.value = ''
            }}
          >
            {t('Remove image')}
          </Button>
        </div>
      )}
    </>
  )
}
