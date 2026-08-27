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
import { describe, expect, test } from 'vitest'

import {
  buildRequestLogDetailHref,
  getRequestLogDetailDisplayBody,
  isJsonLikeErrorBody,
} from '../request-detail'

describe('request log detail navigation', () => {
  test('builds a relative detail URL with an encoded detail id', () => {
    expect(buildRequestLogDetailHref('detail/id?part=1')).toBe(
      '/usage-logs/detail/detail%2Fid%3Fpart%3D1'
    )
  })

  test('preserves the original error response body for syntax highlighting', () => {
    const body = '{"error":{"message":"Unexpected reasoning effort high."}}'

    expect(getRequestLogDetailDisplayBody(body, true)).toBe(body)
  })

  test('keeps an error JSON body eligible for coloring when content type is SSE', () => {
    const body = '{"error":{"message":"Unexpected reasoning effort high."}}'

    expect(isJsonLikeErrorBody(body)).toBe(true)
  })

  test('formats successful JSON responses for readability', () => {
    expect(getRequestLogDetailDisplayBody('{"ok":true}', false)).toBe(
      '{\n  "ok": true\n}'
    )
  })
})
