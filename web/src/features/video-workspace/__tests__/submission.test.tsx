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
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { createVideoTask } from '../api'
import { VideoForm } from '../components/video-form'
import type { VideoWorkspaceCatalog } from '../types'

const catalog: VideoWorkspaceCatalog = {
  quota: 500000,
  models: [
    {
      id: 'sora-2',
      name: 'Sora 2',
      supports_image: true,
      durations: [4, 8],
      sizes: ['1280x720', '720x1280'],
      max_image_bytes: 4,
      max_prompt_length: 4000,
      supported_image_types: ['image/png', 'image/jpeg', 'image/webp'],
    },
    {
      id: 'text-video',
      name: 'Text video',
      supports_image: false,
      max_prompt_length: 4000,
    },
  ],
}

const resolutionCatalog: VideoWorkspaceCatalog = {
  quota: 500000,
  models: [
    {
      id: 'Sd-2.0满血933',
      name: 'Sd-2.0满血933',
      supports_image: false,
      sizes: ['16:9', '9:16'],
      resolutions: ['720p', '1080p', '4k'],
      default_resolution: '720p',
      max_prompt_length: 4000,
    },
    {
      id: 'Sd-2.0mini',
      name: 'Sd-2.0mini',
      supports_image: false,
      resolutions: ['480p', '720p'],
      default_resolution: '480p',
      max_prompt_length: 2000,
    },
    catalog.models[1],
  ],
}

describe('video generation submission', () => {
  test('selecting 4K sends the provider resolution and aspect ratio independently in the multipart request', async () => {
    const user = userEvent.setup()
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      data: {
        id: 'video-4k',
        object: 'video',
        model: 'Sd-2.0满血933',
        status: 'queued',
      },
    })
    render(
      <VideoForm
        catalog={resolutionCatalog}
        pending={false}
        onSubmit={createVideoTask}
      />
    )
    const resolution = screen.getByRole('combobox', {
      name: 'Video resolution',
    })
    expect(resolution).toHaveValue('720p')
    expect(screen.getByRole('option', { name: '4K' })).toHaveValue('4k')
    expect(screen.queryByRole('option', { name: '2K' })).not.toBeInTheDocument()
    await user.selectOptions(resolution, '4k')
    await user.selectOptions(
      screen.getByRole('combobox', { name: 'Aspect ratio' }),
      '9:16'
    )
    await user.type(
      screen.getByRole('textbox', { name: 'Video prompt' }),
      'A river at sunrise'
    )
    await user.click(screen.getByRole('button', { name: 'Generate video' }))

    await waitFor(() => expect(post).toHaveBeenCalledTimes(1))
    const payload = post.mock.calls[0][1] as FormData
    expect(payload.get('resolution')).toBe('4k')
    expect(payload.get('size')).toBe('9:16')
    expect(payload.get('model')).toBe('Sd-2.0满血933')
  })

  test('switching models resets resolution to the supported default and clears it when unsupported', async () => {
    const user = userEvent.setup()
    const onSubmit = vi.fn()
    render(
      <VideoForm
        catalog={resolutionCatalog}
        pending={false}
        onSubmit={onSubmit}
      />
    )
    await user.type(
      screen.getByRole('textbox', { name: 'Video prompt' }),
      'A river'
    )
    await user.selectOptions(
      screen.getByRole('combobox', { name: 'Video resolution' }),
      '4k'
    )
    await user.selectOptions(
      screen.getByRole('combobox', { name: 'Model' }),
      'Sd-2.0mini'
    )

    expect(
      screen.getByRole('combobox', { name: 'Video resolution' })
    ).toHaveValue('480p')
    expect(screen.queryByRole('option', { name: '4K' })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Generate video' }))
    expect(onSubmit).toHaveBeenLastCalledWith(
      expect.objectContaining({
        model: 'Sd-2.0mini',
        prompt: 'A river',
        resolution: '480p',
      })
    )

    await user.selectOptions(
      screen.getByRole('combobox', { name: 'Model' }),
      'text-video'
    )
    expect(
      screen.queryByRole('combobox', { name: 'Video resolution' })
    ).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Generate video' }))
    expect(onSubmit).toHaveBeenLastCalledWith(
      expect.objectContaining({
        model: 'text-video',
        prompt: 'A river',
        resolution: undefined,
      })
    )
  })

  test.each([
    { resolutions: ['720p', '1080p', '4k'], expected: '4k' },
    { resolutions: ['720p', '1080p'], expected: '720p' },
  ])(
    'a catalog refresh with $resolutions keeps only a supported resolution',
    async ({ resolutions, expected }) => {
      const user = userEvent.setup()
      const onSubmit = vi.fn()
      const view = render(
        <VideoForm
          catalog={resolutionCatalog}
          pending={false}
          onSubmit={onSubmit}
        />
      )
      await user.type(
        screen.getByRole('textbox', { name: 'Video prompt' }),
        'A river'
      )
      await user.selectOptions(
        screen.getByRole('combobox', { name: 'Video resolution' }),
        '4k'
      )
      view.rerender(
        <VideoForm
          catalog={{
            quota: 400000,
            models: [{ ...resolutionCatalog.models[0], resolutions }],
          }}
          pending={false}
          onSubmit={onSubmit}
        />
      )
      expect(
        screen.getByRole('combobox', { name: 'Video resolution' })
      ).toHaveValue(expected)
      await user.click(screen.getByRole('button', { name: 'Generate video' }))
      expect(onSubmit).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ prompt: 'A river', resolution: expected })
      )
    }
  )

  test('a provider without an explicit default omits resolution unless the user chooses one', async () => {
    const user = userEvent.setup()
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      data: {
        id: 'video-default',
        object: 'video',
        model: 'Sd-2.0满血933',
        status: 'queued',
      },
    })
    render(
      <VideoForm
        catalog={{
          ...resolutionCatalog,
          models: [
            { ...resolutionCatalog.models[0], default_resolution: undefined },
          ],
        }}
        pending={false}
        onSubmit={createVideoTask}
      />
    )
    expect(
      screen.getByRole('combobox', { name: 'Video resolution' })
    ).toHaveValue('')
    await user.type(
      screen.getByRole('textbox', { name: 'Video prompt' }),
      'A river'
    )
    await user.click(screen.getByRole('button', { name: 'Generate video' }))
    await waitFor(() => expect(post).toHaveBeenCalledTimes(1))
    expect((post.mock.calls[0][1] as FormData).has('resolution')).toBe(false)
  })

  test('a pending submission disables the resolution selector', () => {
    render(<VideoForm catalog={resolutionCatalog} pending onSubmit={vi.fn()} />)
    expect(
      screen.getByRole('combobox', { name: 'Video resolution' })
    ).toBeDisabled()
  })

  test('given a supported image and parameters, submits the selected model and trimmed prompt exactly once', async () => {
    const onSubmit = vi.fn()
    const user = userEvent.setup()
    render(<VideoForm catalog={catalog} pending={false} onSubmit={onSubmit} />)
    const image = new File(['png'], 'frame.png', { type: 'image/png' })
    await user.type(
      screen.getByRole('textbox', { name: 'Video prompt' }),
      '  A river at sunrise  '
    )
    await user.selectOptions(
      screen.getByRole('combobox', { name: 'Video duration' }),
      '8'
    )
    await user.upload(screen.getByLabelText('Reference image'), image)
    await user.click(screen.getByRole('button', { name: 'Generate video' }))

    expect(onSubmit).toHaveBeenCalledExactlyOnceWith({
      model: 'sora-2',
      prompt: 'A river at sunrise',
      seconds: '8',
      size: '1280x720',
      resolution: undefined,
      image,
    })
  })

  test('switching to a prompt-only model preserves the prompt and removes unsupported media and parameters', async () => {
    const onSubmit = vi.fn()
    const user = userEvent.setup()
    render(<VideoForm catalog={catalog} pending={false} onSubmit={onSubmit} />)
    await user.type(
      screen.getByRole('textbox', { name: 'Video prompt' }),
      'A river at sunrise'
    )
    await user.upload(
      screen.getByLabelText('Reference image'),
      new File(['png'], 'frame.png', { type: 'image/png' })
    )
    await user.selectOptions(
      screen.getByRole('combobox', { name: 'Model' }),
      'text-video'
    )

    expect(screen.queryByLabelText('Reference image')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('combobox', { name: 'Video duration' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('combobox', { name: 'Video size' })
    ).not.toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Video prompt' })).toHaveValue(
      'A river at sunrise'
    )
    await user.click(screen.getByRole('button', { name: 'Generate video' }))
    expect(onSubmit).toHaveBeenCalledExactlyOnceWith({
      model: 'text-video',
      prompt: 'A river at sunrise',
      seconds: undefined,
      size: undefined,
      resolution: undefined,
      image: undefined,
    })
  })

  test('removing the selected model during a catalog refresh clears hidden values and retains the prompt', async () => {
    const onSubmit = vi.fn()
    const user = userEvent.setup()
    const view = render(
      <VideoForm catalog={catalog} pending={false} onSubmit={onSubmit} />
    )
    await user.type(
      screen.getByRole('textbox', { name: 'Video prompt' }),
      'A river at sunrise'
    )
    await user.upload(
      screen.getByLabelText('Reference image'),
      new File(['png'], 'frame.png', { type: 'image/png' })
    )

    view.rerender(
      <VideoForm
        catalog={{ ...catalog, models: [catalog.models[1]] }}
        pending={false}
        onSubmit={onSubmit}
      />
    )
    expect(screen.getByRole('combobox', { name: 'Model' })).toHaveValue(
      'text-video'
    )
    expect(screen.queryByLabelText('Reference image')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('combobox', { name: 'Video duration' })
    ).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Generate video' }))
    expect(onSubmit).toHaveBeenCalledExactlyOnceWith({
      model: 'text-video',
      prompt: 'A river at sunrise',
      seconds: undefined,
      size: undefined,
      resolution: undefined,
      image: undefined,
    })

    view.rerender(
      <VideoForm catalog={catalog} pending={false} onSubmit={onSubmit} />
    )
    expect(screen.getByRole('combobox', { name: 'Model' })).toHaveValue(
      'text-video'
    )
  })

  test('changing a model capability profile retains supported duration and prompt while clearing the image and replacing an unavailable size', async () => {
    const onSubmit = vi.fn()
    const user = userEvent.setup()
    const view = render(
      <VideoForm catalog={catalog} pending={false} onSubmit={onSubmit} />
    )
    await user.type(
      screen.getByRole('textbox', { name: 'Video prompt' }),
      'A river at sunrise'
    )
    await user.selectOptions(
      screen.getByRole('combobox', { name: 'Video duration' }),
      '8'
    )
    await user.upload(
      screen.getByLabelText('Reference image'),
      new File(['png'], 'frame.png', { type: 'image/png' })
    )

    view.rerender(
      <VideoForm
        catalog={{
          ...catalog,
          models: [
            {
              ...catalog.models[0],
              supports_image: false,
              durations: [8],
              sizes: ['1920x1080'],
            },
          ],
        }}
        pending={false}
        onSubmit={onSubmit}
      />
    )
    expect(
      screen.getByRole('combobox', { name: 'Video duration' })
    ).toHaveValue('8')
    expect(screen.getByRole('combobox', { name: 'Video size' })).toHaveValue(
      '1920x1080'
    )
    expect(screen.queryByLabelText('Reference image')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Generate video' }))
    expect(onSubmit).toHaveBeenCalledExactlyOnceWith({
      model: 'sora-2',
      prompt: 'A river at sunrise',
      seconds: '8',
      size: '1920x1080',
      resolution: undefined,
      image: undefined,
    })
  })

  test('a balance-only poll with equivalent model capabilities keeps the chosen parameters and image', async () => {
    const onSubmit = vi.fn()
    const user = userEvent.setup()
    const view = render(
      <VideoForm catalog={catalog} pending={false} onSubmit={onSubmit} />
    )
    const image = new File(['png'], 'frame.png', { type: 'image/png' })
    await user.type(
      screen.getByRole('textbox', { name: 'Video prompt' }),
      'A river at sunrise'
    )
    await user.selectOptions(
      screen.getByRole('combobox', { name: 'Video duration' }),
      '8'
    )
    await user.upload(screen.getByLabelText('Reference image'), image)

    view.rerender(
      <VideoForm
        catalog={{
          quota: 400000,
          models: catalog.models.map((model) => ({
            ...model,
            durations: model.durations ? [...model.durations] : undefined,
            sizes: model.sizes ? [...model.sizes] : undefined,
            supported_image_types: model.supported_image_types
              ? [...model.supported_image_types]
              : undefined,
          })),
        }}
        pending={false}
        onSubmit={onSubmit}
      />
    )
    expect(
      screen.getByRole('combobox', { name: 'Video duration' })
    ).toHaveValue('8')
    expect(screen.getByLabelText('Reference image')).not.toHaveValue('')
    await user.click(screen.getByRole('button', { name: 'Generate video' }))
    expect(onSubmit).toHaveBeenCalledExactlyOnceWith({
      model: 'sora-2',
      prompt: 'A river at sunrise',
      seconds: '8',
      size: '1280x720',
      resolution: undefined,
      image,
    })
  })

  test.each([
    [
      'oversized image',
      new File(['large'], 'large.png', { type: 'image/png' }),
      'Image exceeds this model’s upload limit',
    ],
    [
      'unsupported format',
      new File(['svg'], 'frame.svg', { type: 'image/svg+xml' }),
      'Choose a supported image file',
    ],
  ])(
    'an invalid image prevents submission and displays its validation error (%s)',
    async (_name, file, message) => {
      const onSubmit = vi.fn()
      const user = userEvent.setup({ applyAccept: false })
      render(
        <VideoForm catalog={catalog} pending={false} onSubmit={onSubmit} />
      )
      await user.type(
        screen.getByRole('textbox', { name: 'Video prompt' }),
        'A river'
      )
      await user.upload(screen.getByLabelText('Reference image'), file)
      await user.click(screen.getByRole('button', { name: 'Generate video' }))

      expect(await screen.findByText(message)).toBeVisible()
      expect(screen.getByLabelText('Reference image')).toHaveAttribute(
        'aria-invalid',
        'true'
      )
      expect(onSubmit).not.toHaveBeenCalled()
    }
  )

  test('removing an image clears the selected file and excludes it from submission', async () => {
    const onSubmit = vi.fn()
    const user = userEvent.setup()
    render(<VideoForm catalog={catalog} pending={false} onSubmit={onSubmit} />)
    await user.type(
      screen.getByRole('textbox', { name: 'Video prompt' }),
      'A river'
    )
    await user.upload(
      screen.getByLabelText('Reference image'),
      new File(['png'], 'frame.png', { type: 'image/png' })
    )
    await user.click(screen.getByRole('button', { name: 'Remove image' }))
    expect(screen.getByLabelText('Reference image')).toHaveValue('')
    await user.click(screen.getByRole('button', { name: 'Generate video' }))
    expect(onSubmit.mock.calls[0][0].image).toBeUndefined()
  })

  test('a blank prompt announces an accessible validation error without submitting', async () => {
    const onSubmit = vi.fn()
    const user = userEvent.setup()
    render(<VideoForm catalog={catalog} pending={false} onSubmit={onSubmit} />)
    await user.click(screen.getByRole('button', { name: 'Generate video' }))

    expect(await screen.findByText('Enter a video prompt')).toBeVisible()
    expect(
      screen.getByRole('textbox', { name: 'Video prompt' })
    ).toHaveAttribute('aria-invalid', 'true')
    await waitFor(() =>
      expect(
        screen.getByRole('textbox', { name: 'Video prompt' })
      ).toHaveFocus()
    )
    expect(onSubmit).not.toHaveBeenCalled()
  })

  test('a pending submission disables the form so repeated clicks cannot submit another task', () => {
    const onSubmit = vi.fn()
    render(<VideoForm catalog={catalog} pending onSubmit={onSubmit} />)
    expect(screen.getByRole('textbox', { name: 'Video prompt' })).toBeDisabled()
    expect(screen.getByRole('combobox', { name: 'Model' })).toBeDisabled()
    const button = screen.getByRole('button', { name: 'Submitting...' })
    expect(button).toBeDisabled()
    fireEvent.click(button)
    expect(onSubmit).not.toHaveBeenCalled()
  })

  test('zero balance disables generation and explains how to continue', () => {
    render(
      <VideoForm
        catalog={{ ...catalog, quota: 0 }}
        pending={false}
        onSubmit={vi.fn()}
      />
    )
    expect(
      screen.getByRole('button', { name: 'Generate video' })
    ).toBeDisabled()
    expect(screen.getByRole('status')).toHaveTextContent(
      'Insufficient balance. Add funds to generate a video.'
    )
  })
})
