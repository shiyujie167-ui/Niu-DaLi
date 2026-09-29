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
import { createFileRoute, redirect } from '@tanstack/react-router'

import { About } from '@/features/about'
import { parseHeaderNavModulesFromStatus } from '@/lib/nav-modules'
import { statusQueryOptions } from '@/lib/status-query'

export const Route = createFileRoute('/about/')({
  beforeLoad: async ({ context }) => {
    // 「页头导航」关闭「关于」后，直接访问地址也回到首页。
    // 状态读取失败时保持放行，关于页内容本身是公开的。
    const status = await context.queryClient
      .fetchQuery(statusQueryOptions)
      .catch(() => null)
    if (parseHeaderNavModulesFromStatus(status).about === false) {
      throw redirect({ to: '/', replace: true })
    }
  },
  component: About,
})
