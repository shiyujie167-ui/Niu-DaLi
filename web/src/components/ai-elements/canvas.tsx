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
import {
  Background,
  ReactFlow,
  type Edge,
  type Node,
  type ReactFlowProps,
} from '@xyflow/react'
import type { ComponentProps, ReactNode } from 'react'

import '@xyflow/react/dist/style.css'
import { Controls } from './controls'

type CanvasProps<NodeType extends Node, EdgeType extends Edge> = ReactFlowProps<
  NodeType,
  EdgeType
> & {
  children?: ReactNode
  backgroundProps?: ComponentProps<typeof Background>
  controlsProps?: ComponentProps<typeof Controls>
}

export const Canvas = <
  NodeType extends Node = Node,
  EdgeType extends Edge = Edge,
>({
  children,
  backgroundProps,
  controlsProps,
  ...props
}: CanvasProps<NodeType, EdgeType>) => (
  <ReactFlow
    deleteKeyCode={['Backspace', 'Delete']}
    fitView
    panOnDrag={false}
    panOnScroll
    selectionOnDrag
    zoomOnDoubleClick={false}
    {...props}
  >
    <Background bgColor='var(--sidebar)' {...backgroundProps} />
    <Controls {...controlsProps} />
    {children}
  </ReactFlow>
)
