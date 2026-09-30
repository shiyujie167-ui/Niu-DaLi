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
import { useMutation } from '@tanstack/react-query'
import { isAxiosError } from 'axios'
import { useCallback, useEffect, useRef, useState } from 'react'

import { saveCanvas } from './api'
import { serializeCanvas } from './graph'
import type { CanvasDocument, CanvasGraph } from './types'

export function useCanvasDocument(initial: CanvasDocument) {
  const [graph, setGraph] = useState(initial.graph)
  const [past, setPast] = useState<CanvasGraph[]>([])
  const [future, setFuture] = useState<CanvasGraph[]>([])
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<unknown>()
  const [conflict, setConflict] = useState(false)
  const [saved, setSaved] = useState(true)
  const graphRef = useRef(graph)
  const revision = useRef(initial.revision)
  const savedJson = useRef(JSON.stringify(serializeCanvas(initial.graph)))
  const inFlight = useRef<Promise<void> | undefined>(undefined)
  const blocked = useRef(false)
  const mutation = useMutation({
    mutationFn: (input: { revision: number; graph: CanvasGraph }) =>
      saveCanvas(input.revision, input.graph),
    retry: false,
    meta: { errorToast: false },
  })
  const mutateAsync = mutation.mutateAsync

  const checkpoint = useCallback(() => {
    const current = graphRef.current
    setPast((entries) => [...entries.slice(-49), current])
    setFuture([])
  }, [])
  const update = useCallback(
    (change: (value: CanvasGraph) => CanvasGraph, record = true) => {
      const previous = graphRef.current
      const next = change(previous)
      const nextJson = JSON.stringify(serializeCanvas(next))
      if (record && nextJson !== JSON.stringify(serializeCanvas(previous))) {
        setPast((entries) => [...entries.slice(-49), previous])
        setFuture([])
      }
      graphRef.current = next
      setGraph(next)
      setSaved(nextJson === savedJson.current)
    },
    []
  )
  const undo = useCallback(() => {
    const previous = past.at(-1)
    if (!previous) return
    const current = graphRef.current
    setPast(past.slice(0, -1))
    setFuture((entries) => [current, ...entries].slice(0, 50))
    update(() => previous, false)
  }, [past, update])
  const redo = useCallback(() => {
    const next = future[0]
    if (!next) return
    const current = graphRef.current
    setFuture(future.slice(1))
    setPast((entries) => [...entries, current].slice(-50))
    update(() => next, false)
  }, [future, update])

  const save = useCallback(async (): Promise<void> => {
    while (inFlight.current) await inFlight.current
    if (blocked.current) throw new Error('Canvas revision conflict')
    const snapshot = serializeCanvas(graphRef.current)
    const json = JSON.stringify(snapshot)
    if (json === savedJson.current) return
    setSaving(true)
    setSaveError(undefined)
    const request = mutateAsync({ revision: revision.current, graph: snapshot })
      .then((document) => {
        revision.current = document.revision
        savedJson.current = json
        setSaved(JSON.stringify(serializeCanvas(graphRef.current)) === json)
      })
      .catch((error: unknown) => {
        setSaveError(error)
        if (isAxiosError(error) && error.response?.status === 409) {
          blocked.current = true
          setConflict(true)
        }
        throw error
      })
      .finally(() => {
        inFlight.current = undefined
        setSaving(false)
      })
    inFlight.current = request
    await request
  }, [mutateAsync])

  useEffect(() => {
    if (saved || conflict) return
    const timer = window.setTimeout(() => {
      void save().catch(() => undefined)
    }, 900)
    return () => window.clearTimeout(timer)
  }, [graph, saved, conflict, save])
  useEffect(() => {
    if (saved) return
    const warn = (event: BeforeUnloadEvent) => {
      event.preventDefault()
      event.returnValue = ''
    }
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [saved])
  const restore = useCallback((document: CanvasDocument) => {
    graphRef.current = document.graph
    revision.current = document.revision
    savedJson.current = JSON.stringify(serializeCanvas(document.graph))
    blocked.current = false
    setGraph(document.graph)
    setPast([])
    setFuture([])
    setSaved(true)
    setSaveError(undefined)
    setConflict(false)
  }, [])
  return {
    graph,
    graphRef,
    update,
    checkpoint,
    undo,
    redo,
    canUndo: past.length > 0,
    canRedo: future.length > 0,
    save,
    saved,
    saving,
    saveError,
    conflict,
    restore,
  }
}
