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

describe('video generation submission', () => {
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
