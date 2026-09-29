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
import { zodResolver } from '@hookform/resolvers/zod'
import { useEffect, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'

import {
  getVideoParameterDefaults,
  matchingVideoParameterValue,
} from '../lib/parameters'
import { videoFormSchema, type VideoFormValues } from '../lib/schema'
import type { VideoSubmission, VideoWorkspaceCatalog } from '../types'
import { VideoImageInput } from './video-image-input'

interface VideoFormProps {
  catalog: VideoWorkspaceCatalog
  pending: boolean
  onSubmit: (submission: VideoSubmission) => void
}

export function VideoForm(props: VideoFormProps) {
  const { t } = useTranslation()
  const [selectedModel, setSelectedModel] = useState(props.catalog.models[0].id)
  const model =
    props.catalog.models.find((candidate) => candidate.id === selectedModel) ??
    props.catalog.models[0]
  const form = useForm<VideoFormValues>({
    resolver: zodResolver(videoFormSchema(model, t)),
    defaultValues: { prompt: '', ...getVideoParameterDefaults(model) },
  })
  const [imageInputRevision, setImageInputRevision] = useState(0)
  const capabilityProfile = JSON.stringify([
    model.id,
    model.supports_image,
    model.durations,
    model.sizes,
    model.max_image_bytes,
    model.supported_image_types,
  ])
  const previousCapabilityProfile = useRef(capabilityProfile)
  useEffect(() => {
    if (previousCapabilityProfile.current === capabilityProfile) return
    previousCapabilityProfile.current = capabilityProfile
    setSelectedModel(model.id)
    const values = form.getValues()
    const defaults = getVideoParameterDefaults(model)
    const seconds =
      matchingVideoParameterValue(model.durations, values.seconds) ??
      defaults.seconds
    const size =
      matchingVideoParameterValue(model.sizes, values.size) ?? defaults.size
    let image = values.image
    if (
      image &&
      (!model.supports_image ||
        !model.supported_image_types?.includes(image.type) ||
        !model.max_image_bytes ||
        image.size === 0 ||
        image.size > model.max_image_bytes)
    ) {
      image = undefined
      setImageInputRevision((revision) => revision + 1)
    }
    if (
      seconds !== values.seconds ||
      size !== values.size ||
      image !== values.image
    ) {
      form.reset({ ...values, seconds, size, image })
    }
  }, [capabilityProfile, form, model])
  const noBalance = props.catalog.quota <= 0

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit((values) => {
          props.onSubmit({
            model: model.id,
            prompt: values.prompt,
            seconds: matchingVideoParameterValue(
              model.durations,
              values.seconds
            ),
            size: matchingVideoParameterValue(model.sizes, values.size),
            image: model.supports_image ? values.image : undefined,
          })
        })}
      >
        <fieldset disabled={props.pending} className='min-w-0'>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor='video-model'>{t('Model')}</FieldLabel>
              <NativeSelect
                id='video-model'
                value={model.id}
                className='w-full'
                onChange={(event) => {
                  const nextModel = props.catalog.models.find(
                    (candidate) => candidate.id === event.target.value
                  )
                  if (!nextModel) return
                  setSelectedModel(nextModel.id)
                  form.reset({
                    prompt: form.getValues('prompt'),
                    ...getVideoParameterDefaults(nextModel),
                    image: undefined,
                  })
                }}
              >
                {props.catalog.models.map((candidate) => (
                  <NativeSelectOption key={candidate.id} value={candidate.id}>
                    {candidate.name}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </Field>
            <FormField
              control={form.control}
              name='prompt'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Video prompt')}</FormLabel>
                  <FormControl>
                    <Textarea
                      {...field}
                      rows={6}
                      maxLength={model.max_prompt_length || 4000}
                      placeholder={t(
                        'Describe the scene, movement, and visual style'
                      )}
                      className='min-h-36 resize-y'
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            {model.supports_image && (
              <FormField
                control={form.control}
                name='image'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Reference image')}</FormLabel>
                    <VideoImageInput
                      key={`${model.id}:${imageInputRevision}`}
                      file={field.value}
                      onChange={field.onChange}
                      accept={model.supported_image_types?.join(',') ?? ''}
                      maxBytes={model.max_image_bytes ?? 0}
                    />
                    <FormMessage />
                  </FormItem>
                )}
              />
            )}
            <div className='grid gap-4 sm:grid-cols-2'>
              {Boolean(model.durations?.length) && (
                <FormField
                  control={form.control}
                  name='seconds'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Video duration')}</FormLabel>
                      <FormControl>
                        <NativeSelect {...field} className='w-full'>
                          {model.durations?.map((seconds) => (
                            <NativeSelectOption
                              key={seconds}
                              value={String(seconds)}
                            >
                              {t('{{count}} seconds', { count: seconds })}
                            </NativeSelectOption>
                          ))}
                        </NativeSelect>
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              )}
              {Boolean(model.sizes?.length) && (
                <FormField
                  control={form.control}
                  name='size'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Video size')}</FormLabel>
                      <FormControl>
                        <NativeSelect {...field} className='w-full'>
                          {model.sizes?.map((size) => (
                            <NativeSelectOption key={size} value={size}>
                              {size}
                            </NativeSelectOption>
                          ))}
                        </NativeSelect>
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              )}
            </div>
            <p className='text-muted-foreground text-sm'>
              {t(
                'Video generation uses your account balance and the current model pricing.'
              )}
            </p>
            {noBalance && (
              <p className='text-destructive text-sm' role='status'>
                {t('Insufficient balance. Add funds to generate a video.')}
              </p>
            )}
            <Button
              type='submit'
              disabled={props.pending || noBalance}
              className='w-full'
            >
              {props.pending && (
                <Spinner data-icon='inline-start' aria-hidden='true' />
              )}
              {props.pending ? t('Submitting...') : t('Generate video')}
            </Button>
          </FieldGroup>
        </fieldset>
      </form>
    </Form>
  )
}
