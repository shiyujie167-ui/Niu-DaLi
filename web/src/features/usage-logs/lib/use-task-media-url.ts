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
import { useEffect, useState } from 'react'

export type TaskMediaLoader = (
  contentUrl: string,
  signal: AbortSignal
) => Promise<Blob>

interface MediaState {
  source: string
  revision: number
  url?: string
  failed?: boolean
}

/** Keep authenticated media in the open viewer, never in the shared query cache. */
export function useTaskMediaUrl(
  contentUrl: string,
  revision: number,
  loadMedia?: TaskMediaLoader
) {
  const [media, setMedia] = useState<MediaState>()
  useEffect(() => {
    if (!loadMedia) return
    const controller = new AbortController()
    let objectUrl: string | undefined
    void loadMedia(contentUrl, controller.signal)
      .then((blob) => {
        if (controller.signal.aborted) return
        objectUrl = URL.createObjectURL(blob)
        setMedia({ source: contentUrl, revision, url: objectUrl })
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          setMedia({ source: contentUrl, revision, failed: true })
        }
      })
    return () => {
      controller.abort()
      if (objectUrl) URL.revokeObjectURL(objectUrl)
    }
  }, [contentUrl, revision, loadMedia])

  if (!loadMedia) return { url: contentUrl, failed: false, loading: false }
  const current =
    media?.source === contentUrl && media.revision === revision
      ? media
      : undefined
  return {
    url: current?.url,
    failed: current?.failed === true,
    loading: !current,
  }
}
