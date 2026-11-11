import {
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  useReactTable,
} from '@tanstack/react-table'
import type { ColumnDef, RowData, SortingState, VisibilityState } from '@tanstack/react-table'
import { useVirtualizer } from '@tanstack/react-virtual'
import { useEffect, useRef, useState } from 'react'
import { EmptyState, ErrorMessage, LoadingState } from './states'
import { useRowNavigation } from './useRowNavigation'

declare module '@tanstack/react-table' {
  // The type parameters must match TanStack's declaration.
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  interface ColumnMeta<TData extends RowData, TValue> {
    /** A number column: right-aligned with tabular figures, header included. */
    numeric?: boolean
  }
}

const cellClass = (base: string, numeric: boolean | undefined) => (numeric ? `${base} num` : base)

export interface DataTableProps<T> {
  data: T[]
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  columns: ColumnDef<T, any>[]
  getRowId: (row: T) => string
  /** Accessible name of the table. */
  label: string
  isLoading?: boolean
  error?: unknown
  onRetry?: () => void
  emptyMessage?: React.ReactNode
  onRowClick?: (row: T) => void
  /** Called when the last rows scroll into view: fetch the next page. */
  onEndReached?: () => void
  /** Whether another page exists; onEndReached is only called when true. */
  hasNextPage?: boolean
  isFetchingNextPage?: boolean
  rowHeight?: number
  /** Height of the scroll area in px. */
  height?: number
  /**
   * Roving focus over the rows: one row is in the tab order and the arrow
   * keys, j and k, Page Up, Page Down, Home and End move between rows.
   */
  keyboardNavigation?: boolean
}

const OVERSCAN = 8
const END_THRESHOLD = 5

/**
 * Table for long lists (ADR 0030): TanStack Table for columns, sorting of the
 * loaded rows, visibility and sizing; TanStack Virtual keeps only visible rows
 * in the DOM.
 */
export function DataTable<T>({
  data,
  columns,
  getRowId,
  label,
  isLoading = false,
  error,
  onRetry,
  emptyMessage = 'No rows.',
  onRowClick,
  onEndReached,
  hasNextPage = false,
  isFetchingNextPage = false,
  rowHeight = 36,
  height = 480,
  keyboardNavigation = false,
}: DataTableProps<T>) {
  const [sorting, setSorting] = useState<SortingState>([])
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({})
  const scrollRef = useRef<HTMLDivElement>(null)

  // TanStack Table returns unmemoizable functions; the React Compiler skips this component.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data,
    columns,
    getRowId,
    state: { sorting, columnVisibility },
    onSortingChange: setSorting,
    onColumnVisibilityChange: setColumnVisibility,
    columnResizeMode: 'onChange',
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  })
  const rows = table.getRowModel().rows

  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => rowHeight,
    overscan: OVERSCAN,
  })
  const nav = useRowNavigation({
    count: rows.length,
    containerRef: scrollRef,
    scrollToIndex: (index) => virtualizer.scrollToIndex(index),
  })
  const items = virtualizer.getVirtualItems()
  const lastIndex = items.length > 0 ? items[items.length - 1].index : -1

  useEffect(() => {
    if (onEndReached && hasNextPage && !isFetchingNextPage && lastIndex >= rows.length - END_THRESHOLD && rows.length > 0) {
      onEndReached()
    }
  }, [lastIndex, rows.length, onEndReached, hasNextPage, isFetchingNextPage])

  const leaf = table.getVisibleLeafColumns()
  const gridTemplate = leaf.map((c) => `${c.getSize()}px`).join(' ')
  const totalWidth = leaf.reduce((sum, c) => sum + c.getSize(), 0)

  return (
    <div className="data-table">
      <details className="column-menu">
        <summary>Columns</summary>
        <div className="column-list">
          {table.getAllLeafColumns().map((column) => (
            <label key={column.id}>
              <input
                type="checkbox"
                checked={column.getIsVisible()}
                onChange={column.getToggleVisibilityHandler()}
              />
              {typeof column.columnDef.header === 'string' ? column.columnDef.header : column.id}
            </label>
          ))}
        </div>
      </details>
      <div className="table-scroll">
        <div
          role="table"
          aria-label={label}
          aria-rowcount={rows.length + 1}
          aria-busy={isLoading || isFetchingNextPage}
          style={{ minWidth: totalWidth }}
        >
          <div role="rowgroup" className="thead">
            {table.getHeaderGroups().map((group) => (
              <div role="row" key={group.id} className="tr" style={{ gridTemplateColumns: gridTemplate }}>
                {group.headers.map((header) => {
                  const sorted = header.column.getIsSorted()
                  return (
                    <div
                      role="columnheader"
                      key={header.id}
                      className={cellClass('th', header.column.columnDef.meta?.numeric)}
                      aria-sort={sorted === 'asc' ? 'ascending' : sorted === 'desc' ? 'descending' : 'none'}
                    >
                      {header.column.getCanSort() ? (
                        <button type="button" onClick={header.column.getToggleSortingHandler()}>
                          {flexRender(header.column.columnDef.header, header.getContext())}
                          {sorted === 'asc' ? ' ▲' : sorted === 'desc' ? ' ▼' : ''}
                        </button>
                      ) : (
                        flexRender(header.column.columnDef.header, header.getContext())
                      )}
                      {header.column.getCanResize() && (
                        <div
                          className="resizer"
                          aria-hidden="true"
                          onMouseDown={header.getResizeHandler()}
                          onTouchStart={header.getResizeHandler()}
                        />
                      )}
                    </div>
                  )
                })}
              </div>
            ))}
          </div>
          {error ? (
            <StateRow>
              <ErrorMessage error={error} onRetry={onRetry} />
            </StateRow>
          ) : isLoading ? (
            <StateRow>
              <LoadingState />
            </StateRow>
          ) : rows.length === 0 ? (
            <StateRow>
              <EmptyState>{emptyMessage}</EmptyState>
            </StateRow>
          ) : (
            <div ref={scrollRef} className="scroll" style={{ height, overflow: 'auto' }}>
              <div role="rowgroup" style={{ height: virtualizer.getTotalSize(), position: 'relative' }}>
                {items.map((item) => {
                  const row = rows[item.index]
                  return (
                    <div
                      role="row"
                      key={row.id}
                      aria-rowindex={item.index + 2}
                      data-index={item.index}
                      className={onRowClick ? 'tr clickable' : 'tr'}
                      tabIndex={
                        keyboardNavigation ? (item.index === nav.active ? 0 : -1) : onRowClick ? 0 : undefined
                      }
                      onFocus={keyboardNavigation ? () => nav.setActive(item.index) : undefined}
                      onClick={onRowClick ? () => onRowClick(row.original) : undefined}
                      onKeyDown={
                        onRowClick || keyboardNavigation
                          ? (e) => {
                              if (keyboardNavigation && nav.onRowKeyDown(e, item.index)) return
                              if (e.key === 'Enter' && onRowClick) onRowClick(row.original)
                            }
                          : undefined
                      }
                      style={{
                        position: 'absolute',
                        top: 0,
                        height: item.size,
                        transform: `translateY(${item.start}px)`,
                        gridTemplateColumns: gridTemplate,
                      }}
                    >
                      {row.getVisibleCells().map((cell) => (
                        <div role="cell" key={cell.id} className={cellClass('td', cell.column.columnDef.meta?.numeric)}>
                          {flexRender(cell.column.columnDef.cell, cell.getContext())}
                        </div>
                      ))}
                    </div>
                  )
                })}
              </div>
              {isFetchingNextPage && <LoadingState label="Loading more" />}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

/** A table must hold rows: a loading, error or empty message sits in one full-width row. */
function StateRow({ children }: { children: React.ReactNode }) {
  return (
    <div role="rowgroup">
      <div role="row">
        <div role="cell">{children}</div>
      </div>
    </div>
  )
}
