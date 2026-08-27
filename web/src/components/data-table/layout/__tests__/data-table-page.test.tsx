import { createColumnHelper } from '@tanstack/react-table'
import { render } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { useDataTable } from '../../hooks/use-data-table'
import { DataTablePage } from '../data-table-page'

vi.mock('@/hooks', () => ({
  useMediaQuery: () => false,
}))

interface TestRow {
  id: string
}

function Harness({
  disableInteractionWhileFetching,
}: {
  disableInteractionWhileFetching?: boolean
}) {
  const columnHelper = createColumnHelper<TestRow>()
  const { table } = useDataTable({
    data: [{ id: 'row-1' }],
    columns: [columnHelper.accessor('id', { header: 'ID' })],
  })

  return (
    <DataTablePage
      table={table}
      columns={table.options.columns}
      isFetching
      disableInteractionWhileFetching={disableInteractionWhileFetching}
      fixedHeight={false}
    />
  )
}

describe('DataTablePage refresh interaction', () => {
  it('keeps the table interactive when a consumer opts out of the fetching lock', () => {
    const { container } = render(
      <Harness disableInteractionWhileFetching={false} />
    )

    expect(
      container
        .querySelector('[data-slot="table"]')
        ?.closest('.pointer-events-none')
    ).toBeNull()
  })

  it('keeps the existing fetching lock enabled by default', () => {
    const { container } = render(<Harness />)

    expect(
      container
        .querySelector('[data-slot="table"]')
        ?.closest('.pointer-events-none')
    ).not.toBeNull()
  })
})
