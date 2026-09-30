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
import { useQuery } from '@tanstack/react-query'
import {
  applyEdgeChanges,
  applyNodeChanges,
  BackgroundVariant,
  SelectionMode,
  type ReactFlowInstance,
} from '@xyflow/react'
import {
  Copy,
  Hand,
  ImagePlus,
  MousePointer2,
  Redo2,
  Save,
  Trash2,
  Type,
  Undo2,
  Video,
} from 'lucide-react'
import { useRef, useState, type KeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Canvas } from '@/components/ai-elements/canvas'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { handleServerError } from '@/lib/handle-server-error'
import { getServerErrorMessage } from '@/lib/server-error-message'

import type { VideoWorkspaceCatalog } from '../types'
import { getCanvas } from './api'
import { CanvasNodeView } from './canvas-nodes'
import { CanvasContext } from './context'
import {
  cloneCanvasSelection,
  newCanvasNode,
  validCanvasConnection,
} from './graph'
import type {
  CanvasDocument,
  CanvasGraph,
  CanvasNode,
  CanvasNodeKind,
} from './types'
import { useCanvasDocument } from './use-canvas-document'
import { useCanvasTasks } from './use-canvas-tasks'

const nodeTypes = {
  prompt: CanvasNodeView,
  image: CanvasNodeView,
  generation: CanvasNodeView,
}
const backgroundProps = {
  variant: BackgroundVariant.Dots,
  gap: 20,
  size: 1,
  color: '#d4d4d4',
  bgColor: '#fff',
}
const edgeOptions = {
  type: 'default',
  style: { stroke: '#a3a3a3', strokeWidth: 1.5 },
  interactionWidth: 20,
}

export default function VideoCanvas(props: { catalog: VideoWorkspaceCatalog }) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['video-workspace', 'canvas'],
    queryFn: getCanvas,
    retry: false,
    refetchInterval: 5000,
    meta: { errorToast: false },
  })
  if (query.isPending) return <LoadingState className='min-h-80' />
  if (!query.data) {
    return (
      <ErrorState
        title={t('Failed to load canvas')}
        description={getServerErrorMessage(query.error)}
        onRetry={() => void query.refetch()}
      />
    )
  }
  return (
    <CanvasEditor
      catalog={props.catalog}
      initial={query.data}
      refresh={async () => (await query.refetch()).data}
    />
  )
}

function CanvasEditor(props: {
  catalog: VideoWorkspaceCatalog
  initial: CanvasDocument
  refresh: () => Promise<CanvasDocument | undefined>
}) {
  const { t } = useTranslation()
  const document = useCanvasDocument(props.initial)
  const runtime = useCanvasTasks({
    document,
    catalog: props.catalog,
    submissions: props.initial.submissions,
    refresh: props.refresh,
  })
  const [selectMode, setSelectMode] = useState(false)
  const [reloadOpen, setReloadOpen] = useState(false)
  const [reloading, setReloading] = useState(false)
  const flow = useRef<ReactFlowInstance<CanvasNode> | null>(null)
  const surface = useRef<HTMLDivElement>(null)
  const copied = useRef<CanvasGraph | undefined>(undefined)
  const locked = document.conflict || runtime.busyNodes.size > 0
  const selected =
    document.graph.nodes.some((node) => node.selected) ||
    document.graph.edges.some((edge) => edge.selected)

  const remove = (id?: string) => {
    if (locked) return
    document.update((graph) => {
      const ids = new Set(
        graph.nodes
          .filter((node) => (id ? node.id === id : node.selected))
          .map((node) => node.id)
      )
      return {
        ...graph,
        nodes: graph.nodes.filter((node) => !ids.has(node.id)),
        edges: graph.edges.filter(
          (edge) =>
            !ids.has(edge.source) &&
            !ids.has(edge.target) &&
            (id || !edge.selected)
        ),
      }
    })
    surface.current?.focus()
  }
  const duplicate = (source = document.graphRef.current) => {
    if (locked) return
    const selection = cloneCanvasSelection(source)
    if (
      document.graphRef.current.nodes.length + selection.nodes.length > 200 ||
      document.graphRef.current.edges.length + selection.edges.length > 400
    ) {
      toast.error(t('Canvas limit reached: 200 nodes and 400 connections'))
      return
    }
    if (!selection.nodes.length) return
    document.update((graph) => ({
      ...graph,
      nodes: [
        ...graph.nodes.map((node) => ({ ...node, selected: false })),
        ...selection.nodes,
      ],
      edges: [
        ...graph.edges.map((edge) => ({ ...edge, selected: false })),
        ...selection.edges,
      ],
    }))
  }
  const add = (kind: CanvasNodeKind) => {
    if (locked) return
    if (document.graphRef.current.nodes.length >= 200) {
      toast.error(t('Canvas limit reached: 200 nodes and 400 connections'))
      return
    }
    const bounds = surface.current?.getBoundingClientRect()
    const position =
      bounds && flow.current
        ? flow.current.screenToFlowPosition({
            x: bounds.left + Math.max(32, bounds.width / 2 - 160),
            y: bounds.top + 80,
          })
        : { x: 40, y: 40 }
    while (
      document.graphRef.current.nodes.some(
        (node) =>
          Math.abs(node.position.x - position.x) < 40 &&
          Math.abs(node.position.y - position.y) < 40
      )
    ) {
      position.x += 48
      position.y += 48
    }
    const node = newCanvasNode(kind, position, props.catalog.models[0])
    document.update((graph) => ({
      ...graph,
      nodes: [
        ...graph.nodes.map((item) => ({ ...item, selected: false })),
        node,
      ],
    }))
    surface.current?.focus()
  }
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (
      (event.target as HTMLElement).closest(
        'input,textarea,select,[contenteditable="true"],[role="textbox"],[role="dialog"]'
      ) ||
      locked
    ) {
      return
    }
    const modifier = event.metaKey || event.ctrlKey
    const key = event.key.toLowerCase()
    if (modifier && key === 'z') {
      event.preventDefault()
      if (event.shiftKey) document.redo()
      else document.undo()
    } else if (modifier && key === 'y') {
      event.preventDefault()
      document.redo()
    } else if (modifier && key === 'd') {
      event.preventDefault()
      duplicate()
    } else if (modifier && key === 'c') {
      event.preventDefault()
      copied.current = structuredClone(document.graphRef.current)
    } else if (modifier && key === 'v' && copied.current) {
      event.preventDefault()
      duplicate(copied.current)
    } else if (modifier && key === 'a') {
      event.preventDefault()
      document.update(
        (graph) => ({
          ...graph,
          nodes: graph.nodes.map((node) => ({ ...node, selected: true })),
        }),
        false
      )
    } else if (event.key === 'Delete' || event.key === 'Backspace') {
      event.preventDefault()
      remove()
    } else {
      return
    }
    surface.current?.focus()
  }
  const reload = async () => {
    setReloading(true)
    try {
      const remote = await props.refresh()
      if (!remote) throw new Error(t('Failed to load canvas'))
      document.restore(remote)
      void flow.current?.setViewport(remote.graph.viewport)
      setReloadOpen(false)
    } catch (error) {
      handleServerError(error)
    } finally {
      setReloading(false)
    }
  }
  let saveLabel = t('Unsaved changes')
  if (document.saving) saveLabel = t('Saving...')
  else if (document.saved) saveLabel = t('Saved to server')

  return (
    <CanvasContext.Provider
      value={{
        ...runtime,
        catalog: props.catalog,
        graph: document.graph,
        locked,
        deleteNode: remove,
      }}
    >
      <div className='space-y-3' onKeyDown={onKeyDown}>
        <div
          className='flex flex-wrap items-center gap-2'
          role='toolbar'
          aria-label={t('Canvas tools')}
        >
          <Button
            variant='outline'
            size='sm'
            disabled={locked}
            onClick={() => add('prompt')}
          >
            <Type aria-hidden='true' />
            {t('Prompt node')}
          </Button>
          <Button
            variant='outline'
            size='sm'
            disabled={locked}
            onClick={() => add('image')}
          >
            <ImagePlus aria-hidden='true' />
            {t('Image asset')}
          </Button>
          <Button
            variant='outline'
            size='sm'
            disabled={locked}
            onClick={() => add('generation')}
          >
            <Video aria-hidden='true' />
            {t('Video generation')}
          </Button>
          <div className='flex gap-1 rounded-lg border p-0.5'>
            <Button
              variant={selectMode ? 'ghost' : 'secondary'}
              size='icon-sm'
              aria-label={t('Pan canvas')}
              aria-pressed={!selectMode}
              onClick={() => setSelectMode(false)}
            >
              <Hand aria-hidden='true' />
            </Button>
            <Button
              variant={selectMode ? 'secondary' : 'ghost'}
              size='icon-sm'
              aria-label={t('Select nodes')}
              aria-pressed={selectMode}
              onClick={() => setSelectMode(true)}
            >
              <MousePointer2 aria-hidden='true' />
            </Button>
          </div>
          <Button
            variant='ghost'
            size='icon-sm'
            aria-label={t('Undo')}
            disabled={locked || !document.canUndo}
            onClick={document.undo}
          >
            <Undo2 aria-hidden='true' />
          </Button>
          <Button
            variant='ghost'
            size='icon-sm'
            aria-label={t('Redo')}
            disabled={locked || !document.canRedo}
            onClick={document.redo}
          >
            <Redo2 aria-hidden='true' />
          </Button>
          <Button
            variant='ghost'
            size='icon-sm'
            aria-label={t('Duplicate selected nodes')}
            disabled={
              locked || !document.graph.nodes.some((node) => node.selected)
            }
            onClick={() => duplicate()}
          >
            <Copy aria-hidden='true' />
          </Button>
          <Button
            variant='ghost'
            size='icon-sm'
            aria-label={t('Delete selection')}
            disabled={locked || !selected}
            onClick={() => remove()}
          >
            <Trash2 aria-hidden='true' />
          </Button>
          <span role='status' className='text-muted-foreground ms-auto text-xs'>
            {saveLabel}
          </span>
          <Button
            variant='outline'
            size='sm'
            disabled={document.saved || document.saving || document.conflict}
            onClick={() => void document.save().catch(handleServerError)}
          >
            <Save aria-hidden='true' />
            {t('Save')}
          </Button>
        </div>
        {document.saveError != null && (
          <Alert variant='destructive'>
            <AlertTitle>{t('Failed to save canvas')}</AlertTitle>
            <AlertDescription>
              <p>{getServerErrorMessage(document.saveError)}</p>
              {document.conflict && (
                <Button
                  variant='outline'
                  size='sm'
                  onClick={() => setReloadOpen(true)}
                >
                  {t('Reload server canvas')}
                </Button>
              )}
            </AlertDescription>
          </Alert>
        )}
        <div
          ref={surface}
          tabIndex={0}
          role='region'
          aria-label={t('Infinite canvas')}
          className='relative h-[calc(100dvh-17rem)] min-h-[32rem] w-full overflow-hidden rounded-2xl border bg-white outline-none focus-visible:ring-2 focus-visible:ring-blue-500'
        >
          <Canvas<CanvasNode>
            nodes={document.graph.nodes}
            edges={document.graph.edges}
            nodeTypes={nodeTypes}
            defaultViewport={props.initial.graph.viewport}
            fitView={false}
            minZoom={0.1}
            maxZoom={3}
            backgroundProps={backgroundProps}
            defaultEdgeOptions={edgeOptions}
            colorMode='light'
            panOnDrag={selectMode ? [1, 2] : true}
            panOnScroll
            selectionOnDrag={selectMode}
            selectionMode={SelectionMode.Partial}
            nodesDraggable={!locked}
            nodesConnectable={!locked}
            deleteKeyCode={null}
            ariaLabelConfig={{
              'controls.ariaLabel': t('Canvas tools'),
              'controls.zoomIn.ariaLabel': t('Zoom in'),
              'controls.zoomOut.ariaLabel': t('Zoom out'),
              'controls.fitView.ariaLabel': t('Fit view'),
              'controls.interactive.ariaLabel': t('Toggle canvas interaction'),
            }}
            onInit={(instance) => {
              flow.current = instance
            }}
            onPaneClick={() => surface.current?.focus()}
            onNodeDragStart={document.checkpoint}
            onSelectionDragStart={document.checkpoint}
            onNodesChange={(changes) =>
              document.update(
                (graph) => ({
                  ...graph,
                  nodes: applyNodeChanges(changes, graph.nodes),
                }),
                changes.some(
                  (change) =>
                    change.type === 'position' && change.dragging === undefined
                )
              )
            }
            onEdgesChange={(changes) =>
              document.update(
                (graph) => ({
                  ...graph,
                  edges: applyEdgeChanges(changes, graph.edges),
                }),
                changes.some((change) => change.type === 'remove')
              )
            }
            onMoveEnd={(_, viewport) =>
              document.update((graph) => ({ ...graph, viewport }), false)
            }
            isValidConnection={(connection) =>
              validCanvasConnection(connection, document.graphRef.current)
            }
            onConnect={(connection) => {
              if (
                locked ||
                !validCanvasConnection(connection, document.graphRef.current)
              ) {
                return
              }
              if (document.graphRef.current.edges.length >= 400) {
                toast.error(
                  t('Canvas limit reached: 200 nodes and 400 connections')
                )
                return
              }
              document.update((graph) => ({
                ...graph,
                edges: [
                  ...graph.edges,
                  { ...connection, id: crypto.randomUUID() },
                ],
              }))
            }}
          />
          {document.graph.nodes.length === 0 && (
            <div className='pointer-events-none absolute inset-0 flex items-center justify-center p-6'>
              <div className='max-w-sm space-y-2 rounded-2xl border bg-white/95 p-6 text-center text-neutral-900 shadow-sm'>
                <p className='font-medium'>
                  {t('Start with a prompt, image or video node')}
                </p>
                <p className='text-sm text-neutral-500'>
                  {t(
                    'Connect materials to a generation node to use them as real inputs.'
                  )}
                </p>
              </div>
            </div>
          )}
        </div>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Drag to pan, scroll to move, pinch to zoom. Shift-drag to select. Ctrl/Cmd+C/V to copy, Delete to remove, Ctrl/Cmd+Z to undo.'
          )}
        </p>
        <ConfirmDialog
          open={reloadOpen}
          onOpenChange={setReloadOpen}
          title={t('Reload server canvas')}
          desc={t(
            'Unsaved local changes will be replaced by the server canvas.'
          )}
          confirmText={t('Reload')}
          handleConfirm={() => void reload()}
          isLoading={reloading}
        />
      </div>
    </CanvasContext.Provider>
  )
}
