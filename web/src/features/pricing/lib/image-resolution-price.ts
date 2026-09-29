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
import { flattenBinary } from './billing-expression/display'
import { compileBillingExpression } from './billing-expression/parser'
import { evaluateBillingExpression } from './billing-expression/runtime'

function imageResolutionLabel(size: string): string | null {
  const short = /^([1-9]\d*)k$/i.exec(size)
  if (short) {
    const scale = Number(short[1])
    return Number.isSafeInteger(scale * 1024) ? `${scale}K` : null
  }
  const pixels = /^(\d+)x(\d+)$/.exec(size)
  if (!pixels || pixels[1] !== pixels[2]) return null
  const width = Number(pixels[1])
  if (!Number.isSafeInteger(width) || width < 1024 || width % 1024 !== 0) {
    return null
  }
  return `${width / 1024}K`
}

/** Resolve complete per-image size rules without guessing other request context. */
export function getImageResolutionPrices(
  expression: string
): { label: string; price: number }[] | null {
  const compiled = compileBillingExpression(expression)
  if (compiled.status !== 'ready') return null

  let imageCountFactors = 0
  let baseSize: string | null = null
  const sizes = new Set<string>()
  for (const factor of flattenBinary(compiled.ast, '*')) {
    if (factor.kind === 'variable' && factor.name === 'image_count') {
      imageCountFactors++
      continue
    }
    if (factor.kind === 'call' && factor.name === 'tier') {
      if (baseSize !== null) return null
      const label = factor.args[0]
      const price = factor.args[1]
      if (
        label.kind !== 'literal' ||
        typeof label.value !== 'string' ||
        !imageResolutionLabel(label.value) ||
        price.kind !== 'call' ||
        price.name !== 'fixed'
      ) {
        return null
      }
      baseSize = label.value
      sizes.add(baseSize)
      continue
    }
    const rule = compiled.requestRules.find((item) => item.node === factor)
    if (!rule || !Number.isFinite(rule.multiplier) || rule.multiplier < 0) {
      return null
    }
    for (const condition of flattenBinary(rule.condition, '||')) {
      if (condition.kind !== 'binary' || condition.operator !== '==') {
        return null
      }
      let probe = condition.left
      let value = condition.right
      if (probe.kind === 'literal') {
        probe = condition.right
        value = condition.left
      }
      if (
        probe.kind !== 'call' ||
        probe.name !== 'param' ||
        probe.args[0].kind !== 'literal' ||
        probe.args[0].value !== 'size' ||
        value.kind !== 'literal' ||
        typeof value.value !== 'string' ||
        !imageResolutionLabel(value.value)
      ) {
        return null
      }
      sizes.add(value.value)
    }
  }
  if (imageCountFactors !== 1 || baseSize === null) return null

  const fallback = evaluateBillingExpression(compiled, {
    imageCount: 1,
    request: { body: {} },
  })
  if (fallback.status !== 'success' || fallback.billingUnit !== 'request') {
    return null
  }
  // A normalized resolution label must quote the same price for every alias,
  // including spellings omitted from the rule list that use the fallback.
  const configuredSizes = [...sizes]
  for (const size of configuredSizes) {
    const label = imageResolutionLabel(size)
    if (!label) return null
    const pixels = Number.parseInt(label) * 1024
    sizes.add(label)
    sizes.add(label.toLowerCase())
    sizes.add(`${pixels}x${pixels}`)
  }
  const prices = new Map<string, number>()
  for (const size of sizes) {
    const label = imageResolutionLabel(size)
    if (!label) return null
    const result = evaluateBillingExpression(compiled, {
      imageCount: 1,
      request: { body: { size } },
    })
    if (result.status !== 'success' || result.billingUnit !== 'request') {
      return null
    }
    if (size === baseSize && result.cost !== fallback.cost) return null
    const price = result.cost / 1_000_000
    if (!Number.isFinite(price) || price < 0) return null
    const previous = prices.get(label)
    if (previous !== undefined && previous !== price) return null
    prices.set(label, price)
  }

  return [...prices]
    .sort(([left], [right]) => Number.parseInt(left) - Number.parseInt(right))
    .map(([label, price]) => ({ label, price }))
}
