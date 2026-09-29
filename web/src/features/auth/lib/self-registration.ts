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
type SelfRegistrationStatus = {
  self_use_mode_enabled?: unknown
  register_enabled?: unknown
}

/**
 * Whether visitors may create their own account.
 *
 * Mirrors the backend `RegisterEnabled` option, which is what actually rejects
 * registration requests; this only decides whether sign-up entry points are
 * offered. An unknown status stays open so a failed status read cannot hide
 * sign-up on a site that allows it.
 */
export function isSelfRegistrationOpen(
  status: SelfRegistrationStatus | null | undefined
): boolean {
  return !status?.self_use_mode_enabled && status?.register_enabled !== false
}
