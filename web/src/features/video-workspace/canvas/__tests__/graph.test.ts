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
import i18next from 'i18next'
import { describe, expect, test } from 'vitest'

import type { TaskLog } from '@/features/usage-logs/types'

import type { VideoWorkspaceModel } from '../../types'
import {
  cloneCanvasSelection,
  compileCanvasVideo,
  latestCanvasSubmissions,
  nodeTaskId,
  serializeCanvas,
  validCanvasConnection,
} from '../graph'
import type { CanvasGraph, CanvasSubmission } from '../types'

const model: VideoWorkspaceModel = {
  id: 'video-model',
  name: 'Video model',
  supports_image: true,
  max_reference_images: 2,
  supports_video: true,
  max_reference_videos: 1,
  max_outputs: 1,
  supports_mixed_media: false,
  max_prompt_length: 40,
  supported_image_types: ['image/png'],
  max_image_bytes: 1024,
  durations: [5, 10],
  sizes: ['16:9'],
  resolutions: ['720p'],
}
const completed: TaskLog = {
  id: 1,
  user_id: 1,
  platform: 'provider',
  task_id: 'task-output',
  action: 'TEXT_TO_VIDEO',
  channel_id: 1,
  group: 'default',
  quota: 10,
  submit_time: 1,
  status: 'SUCCESS',
}

function graphFixture(): CanvasGraph {
  return {
    schema_version: 1,
    viewport: { x: 15, y: -20, zoom: 0.75 },
    nodes: [
      {
        id: 'prompt',
        type: 'prompt',
        position: { x: 0, y: 0 },
        data: { prompt: 'A sunrise' },
      },
      {
        id: 'image',
        type: 'image',
        position: { x: 0, y: 200 },
        data: { asset_id: 'asset-1', mime_type: 'image/png', size: 100 },
      },
      {
        id: 'generate',
        type: 'generation',
        position: { x: 400, y: 0 },
        data: {
          model: 'video-model',
          prompt: 'over the sea',
          seconds: '5',
          size: '16:9',
          resolution: '720p',
          count: 1,
        },
      },
    ],
    edges: [
      { id: 'prompt-edge', source: 'prompt', target: 'generate' },
      { id: 'image-edge', source: 'image', target: 'generate' },
    ],
  }
}

describe('canvas generation input contract', () => {
  test('connected prompts and saved images become actual generation parameters', () => {
    const graph = graphFixture()
    graph.nodes.push({
      id: 'second-image',
      type: 'image',
      position: { x: 0, y: 400 },
      data: { asset_id: 'asset-2', mime_type: 'image/png', size: 200 },
    })
    graph.edges.push({
      id: 'second-image-edge',
      source: 'second-image',
      target: 'generate',
    })
    expect(
      compileCanvasVideo(
        graph,
        'generate',
        [model],
        new Map(),
        new Map(),
        i18next.t
      )
    ).toEqual({
      model: 'video-model',
      prompt: 'A sunrise\n\nover the sea',
      seconds: '5',
      size: '16:9',
      resolution: '720p',
      n: 1,
      image_asset_ids: ['asset-1', 'asset-2'],
      video_references: [],
      canvas_node_id: 'generate',
    })
  })

  test('completed video output becomes a task artifact reference for the next node', () => {
    const graph = graphFixture()
    graph.nodes[1] = {
      id: 'image',
      type: 'generation',
      position: { x: 0, y: 200 },
      data: { task_id: 'task-output', artifact_key: 'video' },
    }
    const request = compileCanvasVideo(
      graph,
      'generate',
      [model],
      new Map([[completed.task_id, completed]]),
      new Map(),
      i18next.t
    )
    expect(request.video_references).toEqual([
      { task_id: 'task-output', artifact_key: 'video' },
    ])
    expect(request.image_asset_ids).toEqual([])
  })

  test('an unfinished connected video blocks submission instead of dropping the input', () => {
    const graph = graphFixture()
    graph.nodes[1] = {
      id: 'image',
      type: 'generation',
      position: { x: 0, y: 200 },
      data: { task_id: 'task-output', artifact_key: 'video' },
    }
    expect(() =>
      compileCanvasVideo(
        graph,
        'generate',
        [model],
        new Map([[completed.task_id, { ...completed, status: 'IN_PROGRESS' }]]),
        new Map(),
        i18next.t
      )
    ).toThrow('Wait for connected videos')
  })

  test.each([
    ['seconds', '7', 'Choose a supported duration'],
    ['size', '4:3', 'Choose a supported video size'],
    ['resolution', '4k', 'Choose a supported video resolution'],
    ['count', 2, 'Choose a supported output quantity'],
    ['count', 0, 'Choose a supported output quantity'],
    ['count', 1.5, 'Choose a supported output quantity'],
  ])(
    'unsupported %s=%s is rejected before a chargeable request',
    (key, value, message) => {
      const graph = graphFixture()
      graph.nodes[2].data[key] = value
      expect(() =>
        compileCanvasVideo(
          graph,
          'generate',
          [model],
          new Map(),
          new Map(),
          i18next.t
        )
      ).toThrow(message)
    }
  )

  test('unsupported image count is rejected using the selected model capability', () => {
    const graph = graphFixture()
    expect(() =>
      compileCanvasVideo(
        graph,
        'generate',
        [{ ...model, supports_image: false, max_reference_images: 0 }],
        new Map(),
        new Map(),
        i18next.t
      )
    ).toThrow('reference images')
  })

  test('an unuploaded connected image blocks submission', () => {
    const graph = graphFixture()
    graph.nodes[1].data.asset_id = ''
    expect(() =>
      compileCanvasVideo(
        graph,
        'generate',
        [model],
        new Map(),
        new Map(),
        i18next.t
      )
    ).toThrow('Upload every connected image')
  })

  test('combined image and video inputs are rejected when the model forbids mixed media', () => {
    const graph = graphFixture()
    graph.nodes.push({
      id: 'video',
      type: 'generation',
      position: { x: 0, y: 400 },
      data: { task_id: 'task-output', artifact_key: 'video' },
    })
    graph.edges.push({ id: 'video-edge', source: 'video', target: 'generate' })
    expect(() =>
      compileCanvasVideo(
        graph,
        'generate',
        [model],
        new Map([[completed.task_id, completed]]),
        new Map(),
        i18next.t
      )
    ).toThrow('cannot combine image and video')
  })

  test('prompt length is checked after combining every connected prompt', () => {
    const graph = graphFixture()
    graph.nodes[0].data.prompt = 'A'.repeat(40)
    expect(() =>
      compileCanvasVideo(
        graph,
        'generate',
        [model],
        new Map(),
        new Map(),
        i18next.t
      )
    ).toThrow('Video prompt is too long')
  })
})

describe('canvas graph editing and restore', () => {
  test('copy preserves selected internal connections and strips generated task associations', () => {
    const graph = graphFixture()
    graph.nodes[0].selected = true
    graph.nodes[2].selected = true
    graph.nodes[2].data.task_id = 'task-output'
    graph.nodes[2].data.artifact_key = 'video'
    const copied = cloneCanvasSelection(graph)
    expect(copied.nodes).toHaveLength(2)
    expect(copied.nodes[0].position).toEqual({ x: 48, y: 48 })
    expect(copied.nodes[1].data.task_id).toBeUndefined()
    expect(copied.nodes[1].data.artifact_key).toBeUndefined()
    expect(copied.edges).toHaveLength(1)
    expect(copied.edges[0]).toMatchObject({
      source: copied.nodes[0].id,
      target: copied.nodes[1].id,
    })
    expect(graph.nodes[2].data.task_id).toBe('task-output')
  })

  test('cyclic, duplicate and non-generation target connections are rejected', () => {
    const graph = graphFixture()
    graph.nodes.push({
      id: 'next',
      type: 'generation',
      position: { x: 800, y: 0 },
      data: { model: model.id },
    })
    graph.edges.push({ id: 'next-edge', source: 'generate', target: 'next' })
    expect(
      validCanvasConnection(
        {
          source: 'next',
          target: 'generate',
          sourceHandle: null,
          targetHandle: null,
        },
        graph
      )
    ).toBe(false)
    expect(
      validCanvasConnection(
        {
          source: 'prompt',
          target: 'generate',
          sourceHandle: null,
          targetHandle: null,
        },
        graph
      )
    ).toBe(false)
    expect(
      validCanvasConnection(
        {
          source: 'next',
          target: 'image',
          sourceHandle: null,
          targetHandle: null,
        },
        graph
      )
    ).toBe(false)
    expect(
      validCanvasConnection(
        {
          source: 'image',
          target: 'next',
          sourceHandle: null,
          targetHandle: null,
        },
        graph
      )
    ).toBe(true)
  })

  test('serialized graph keeps layout, viewport and task references without transient selection', () => {
    const graph = graphFixture()
    graph.nodes[2].selected = true
    graph.nodes[2].measured = { width: 300, height: 400 }
    graph.nodes[2].data.task_id = 'task-output'
    const saved = serializeCanvas(graph)
    expect(saved.viewport).toEqual({ x: 15, y: -20, zoom: 0.75 })
    expect(saved.nodes[2]).toMatchObject({
      position: { x: 400, y: 0 },
      data: { task_id: 'task-output' },
    })
    expect(saved.nodes[2]).not.toHaveProperty('selected')
    expect(saved.nodes[2]).not.toHaveProperty('measured')
    expect(saved.edges).toEqual(graph.edges)
  })

  test('newest persisted submission wins when multiple records have the same timestamp', () => {
    const newest: CanvasSubmission = {
      node_id: 'generate',
      submission_id: 'new',
      task_id: '',
      status: 'unknown',
      created_at: 5,
      updated_at: 5,
    }
    const older: CanvasSubmission = {
      ...newest,
      submission_id: 'old',
      task_id: 'task-output',
      status: 'accepted',
    }
    const records = latestCanvasSubmissions([newest, older])
    expect(records.get('generate')?.submission_id).toBe('new')
    const node = graphFixture().nodes[2]
    node.data.task_id = 'task-output'
    expect(nodeTaskId(node, records)).toBeUndefined()
  })
})
