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
import { useMutation, useQueries, useQueryClient } from '@tanstack/react-query'
import { isAxiosError } from 'axios'
import { useCallback, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import type { TaskLog } from '@/features/usage-logs/types'
import { handleServerError } from '@/lib/handle-server-error'

import type { VideoWorkspaceCatalog } from '../types'
import { createCanvasVideo, getCanvasTask, uploadCanvasAsset } from './api'
import {
  compileCanvasVideo,
  latestCanvasSubmissions,
  nodeTaskId,
} from './graph'
import type { CanvasDocument, CanvasNodeData, CanvasSubmission } from './types'
import type { useCanvasDocument } from './use-canvas-document'

type LocalCanvasSubmission = CanvasSubmission & {
  previous_submission_id?: string
}

export function useCanvasTasks(props: {
  document: ReturnType<typeof useCanvasDocument>
  catalog: VideoWorkspaceCatalog
  submissions: CanvasSubmission[]
  refresh: () => Promise<CanvasDocument | undefined>
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [localRuns, setLocalRuns] = useState(
    new Map<string, LocalCanvasSubmission>()
  )
  const [busyNodes, setBusyNodes] = useState(new Set<string>())
  const [errors, setErrors] = useState(new Map<string, unknown>())
  const pending = useRef(new Set<string>())
  const { update, graphRef, save } = props.document
  const submissions = useMemo(() => {
    const records = latestCanvasSubmissions(props.submissions)
    for (const [id, local] of localRuns) {
      const remote = records.get(id)
      if (!remote || remote.submission_id === local.previous_submission_id) {
        records.set(id, local)
      } else if (
        remote.submission_id === local.submission_id &&
        remote.status === 'submitting' &&
        local.status !== 'submitting'
      ) {
        records.set(id, local)
      }
    }
    return records
  }, [props.submissions, localRuns])
  const taskIds = [
    ...new Set(
      props.document.graph.nodes
        .map((node) => nodeTaskId(node, submissions))
        .filter((id): id is string => Boolean(id))
    ),
  ]
  const queries = useQueries({
    queries: taskIds.map((id) => ({
      queryKey: ['video-workspace', 'canvas-task', id],
      queryFn: () => getCanvasTask(id),
      refetchInterval: (query: { state: { data?: TaskLog } }) =>
        query.state.data &&
        ['SUCCESS', 'FAILURE'].includes(query.state.data.status)
          ? (false as const)
          : 5000,
      retry: false,
      meta: { errorToast: false },
    })),
  })
  const tasks = new Map<string, TaskLog>()
  const taskErrors = new Map<string, unknown>()
  queries.forEach((query, index) => {
    if (query.data) tasks.set(taskIds[index], query.data)
    if (query.isError) taskErrors.set(taskIds[index], query.error)
  })
  const upload = useMutation({
    mutationFn: uploadCanvasAsset,
    retry: false,
    meta: { errorToast: false },
  })
  const submit = useMutation({
    mutationFn: createCanvasVideo,
    retry: false,
    meta: { errorToast: false },
  })
  const updateNode = useCallback(
    (id: string, data: Partial<CanvasNodeData>) => {
      update((graph) => ({
        ...graph,
        nodes: graph.nodes.map((node) =>
          node.id === id ? { ...node, data: { ...node.data, ...data } } : node
        ),
      }))
    },
    [update]
  )

  const uploadImage = async (id: string, file: File) => {
    if (pending.current.has(id) || props.document.conflict) return
    if (
      !['image/png', 'image/jpeg', 'image/webp'].includes(file.type) ||
      file.size <= 0 ||
      file.size > 10 * 1024 * 1024
    ) {
      toast.error(t('Upload a PNG, JPEG or WebP image up to 10 MiB'))
      return
    }
    pending.current.add(id)
    setBusyNodes(new Set(pending.current))
    try {
      const asset = await upload.mutateAsync(file)
      updateNode(id, {
        asset_id: asset.id,
        filename: asset.filename,
        mime_type: asset.mime_type,
        size: asset.size,
        width: asset.width,
        height: asset.height,
        content_url: asset.content_url,
      })
      await save()
    } catch (error) {
      handleServerError(error)
    } finally {
      pending.current.delete(id)
      setBusyNodes(new Set(pending.current))
    }
  }

  const generate = async (id: string) => {
    if (pending.current.has(id) || props.document.conflict) return
    const prior = submissions.get(id)
    const node = graphRef.current.nodes.find((item) => item.id === id)
    const existingId = node && nodeTaskId(node, submissions)
    if (
      prior?.status === 'submitting' ||
      prior?.status === 'unknown' ||
      (existingId &&
        !['SUCCESS', 'FAILURE'].includes(tasks.get(existingId)?.status ?? ''))
    ) {
      return
    }
    pending.current.add(id)
    setBusyNodes(new Set(pending.current))
    setErrors((value) => {
      const next = new Map(value)
      next.delete(id)
      return next
    })
    let claim: LocalCanvasSubmission | undefined
    try {
      const request = compileCanvasVideo(
        graphRef.current,
        id,
        props.catalog.models,
        tasks,
        submissions,
        t
      )
      await save()
      const now = Math.floor(Date.now() / 1000)
      const started: LocalCanvasSubmission = {
        node_id: id,
        submission_id: crypto.randomUUID(),
        // Identify the stale receipt explicitly; client and server clocks can differ.
        previous_submission_id: prior?.submission_id,
        task_id: '',
        status: 'submitting',
        created_at: now,
        updated_at: now,
      }
      claim = started
      setLocalRuns((value) => new Map(value).set(id, started))
      const receipt = await submit.mutateAsync({
        ...request,
        submission_id: claim.submission_id,
      })
      const accepted = {
        ...claim,
        status: 'accepted' as const,
        task_id: receipt.id,
      }
      setLocalRuns((value) => new Map(value).set(id, accepted))
      updateNode(id, { task_id: receipt.id, artifact_key: undefined })
      toast.success(t('Video task submitted'))
    } catch (error) {
      setErrors((value) => new Map(value).set(id, error))
      if (claim) {
        const status =
          isAxiosError(error) &&
          error.response &&
          error.response.status >= 400 &&
          error.response.status < 500 &&
          error.response.status !== 409
            ? 'failed'
            : 'unknown'
        const failed: LocalCanvasSubmission = { ...claim, status }
        if (isAxiosError(error) && error.response?.status === 409) {
          // The server already owns this node's submission, possibly from another tab.
          // Its receipt must win even when it predates this client's attempted claim.
          delete failed.previous_submission_id
        }
        setLocalRuns((value) => new Map(value).set(id, failed))
      }
      handleServerError(error)
    } finally {
      if (claim) {
        await props.refresh().catch(() => undefined)
        void client.invalidateQueries({
          queryKey: ['video-workspace', 'models'],
        })
        void client.invalidateQueries({
          queryKey: ['video-workspace', 'tasks'],
        })
      }
      pending.current.delete(id)
      setBusyNodes(new Set(pending.current))
    }
  }

  const refresh = () => {
    void props.refresh().catch(handleServerError)
    void client.invalidateQueries({
      queryKey: ['video-workspace', 'canvas-task'],
    })
  }
  return {
    tasks,
    taskErrors,
    submissions,
    busyNodes,
    errors,
    updateNode,
    uploadImage,
    generate,
    refresh,
  }
}
