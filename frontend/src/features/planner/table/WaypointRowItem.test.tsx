// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { renderToString } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'
import { expedition } from '../../../api/models'
import { WaypointRowItem, type WaypointRowHandlers } from './WaypointRowItem'

;(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const defaultRowHandlers: WaypointRowHandlers = {
  onToggleExpand: () => {},
  onFieldBlur: () => {},
  onCopySystem: () => {},
  onToggleFcStop: () => {},
  onOpenCarrierRoutePrompt: () => {},
  onToggleInsertMenu: () => {},
  onInsert: () => {},
  onDelete: () => {},
  onDragStart: () => {},
  onDragEnd: () => {},
}

interface MakeWaypointRowOptions {
  row?: Partial<expedition.Row>
  number?: number
  visitState?: string
  visitedAtGalactic?: string
  departedAtGalactic?: string
  distFromPrevLy?: number
  distFromCurrentLy?: number
  isNextWaypoint?: boolean
  legModeId?: number
  legModeColor?: string
  detectedRegion?: string
  visitCount?: number
  allowInsertWaypoint?: boolean
  permissions?: Partial<expedition.WaypointPermissions>
}

function makeWaypointRow(options: MakeWaypointRowOptions = {}): expedition.RowView {
  const visitState = options.visitState ?? 'future'
  const number = options.number ?? 2
  const isVisited = visitState === 'visited'
  const isOrigin = number === 1 && isVisited
  return expedition.RowView.createFrom({
    row: {
      id: 10,
      expeditionId: 1,
      rowType: 'waypoint',
      systemName: 'Sol',
      ...options.row,
    },
    number,
    visitState,
    visitedAtGalactic: options.visitedAtGalactic ?? '',
    departedAtGalactic: options.departedAtGalactic,
    distFromPrevLy: options.distFromPrevLy,
    distFromCurrentLy: options.distFromCurrentLy,
    isNextWaypoint: options.isNextWaypoint,
    legModeId: options.legModeId ?? 0,
    legModeColor: options.legModeColor ?? '',
    detectedRegion: options.detectedRegion ?? '',
    visitCount: options.visitCount ?? 0,
    allowInsertWaypoint: options.allowInsertWaypoint ?? true,
    permissions: {
      systemLocked: isVisited || isOrigin,
      allowInsertWaypoint: options.allowInsertWaypoint ?? true,
      canReorder: !isOrigin,
      deleteDisabledReason: isOrigin ? 'Origin waypoint cannot be deleted' : undefined,
      reorderDisabledReason: isOrigin ? 'Origin waypoint cannot be reordered' : undefined,
      canAddFcStop: true,
      canRemoveFcStop: true,
      canOverrideFcBadge: true,
      hasDrawer: visitState !== 'future',
      ...options.permissions,
    },
  })
}

function renderRowItem(
  props: Partial<Parameters<typeof WaypointRowItem>[0]> & { rowView: expedition.RowView },
): string {
  const defaultProps: Parameters<typeof WaypointRowItem>[0] = {
    index: 1,
    isPlanning: false,
    isLocked: false,
    isBeingDragged: false,
    isExpanded: false,
    isSystemCopied: false,
    insertMenuOpen: false,
    lastVisitedIndex: 0,
    rowStyle: {},
    dropProps: {},
    handlers: defaultRowHandlers,
    ...props,
  }
  return renderToString(
    createElement(
      'table',
      null,
      createElement('tbody', null, createElement(WaypointRowItem, defaultProps)),
    ),
  )
}

function setFormControlValue(
  element: HTMLInputElement | HTMLTextAreaElement,
  nextValue: string,
): void {
  const prototype =
    element instanceof HTMLTextAreaElement
      ? HTMLTextAreaElement.prototype
      : HTMLInputElement.prototype
  const descriptor = Object.getOwnPropertyDescriptor(prototype, 'value')
  descriptor?.set?.call(element, nextValue)
  element.dispatchEvent(new Event('input', { bubbles: true }))
}

describe('WaypointRowItem and AutoExpandingTextarea Row Height Consistency', () => {
  it('renders visited row with uniform h-7 (28px) elements, align-top cells, and inset shadow left border', () => {
    const html = renderRowItem({
      rowView: makeWaypointRow({
        row: {
          id: 11,
          systemName: 'Sol',
          poiName: 'Earth',
          notes: 'Returning to Bubble to participate in event',
        },
        number: 11,
        visitState: 'visited',
        visitedAtGalactic: '3310-02-15 14:32',
        permissions: { systemLocked: true, canReorder: false, canAddFcStop: false, hasDrawer: true },
      }),
      index: 10,
      isLocked: true,
      lastVisitedIndex: 10,
    })

    expect(html).toContain('align-top')
    expect(html).toContain('py-1.5')
    expect(html).toContain('style="border-left-color:#FF7100"')
    expect(html).toContain('border-l-2 shadow-[inset_0_-1px_0_0_var(--color-ed-border-subtle)]')
    expect(html).toContain('h-7 flex items-center justify-center')
    expect(html).toContain(
      'h-7 flex items-center justify-end font-mono-tabular font-bold text-ed-amber text-xs leading-5',
    )
    expect(html).toContain(
      'w-full h-7 flex items-center font-mono-tabular font-bold text-ed-text-main px-1.5 py-[3px] border border-transparent select-text cursor-text truncate text-xs leading-5',
    )
    expect(html).toContain('h-7 inline-flex items-center justify-center gap-1.5 whitespace-nowrap')
    expect(html).toContain(
      'h-7 flex items-center gap-1.5 whitespace-nowrap font-mono-tabular text-[10px] leading-5',
    )
    expect(html).toContain('h-7 inline-flex items-center justify-end gap-1 whitespace-nowrap')
    expect(html).toContain('min-h-[28px]')
    expect(html).toContain('py-[3px]')
    expect(html).toContain('rows="1"')
    expect(html).not.toContain('rows="2"')
  })

  it('renders unvisited row with matching h-7 (28px) elements and input box model', () => {
    const html = renderRowItem({
      rowView: makeWaypointRow({
        row: { id: 17, systemName: 'LFT 509', poiName: 'Outpost', notes: '' },
        number: 17,
        visitState: 'future',
        distFromPrevLy: 42.5,
      }),
      index: 16,
      lastVisitedIndex: 10,
    })

    expect(html).toContain('w-full h-7 bg-transparent px-1.5 py-[3px] border border-transparent')
    expect(html).toContain('h-7 flex items-center whitespace-nowrap font-mono-tabular text-[11px]')
    expect(html).toContain('h-7 inline-flex items-center justify-end gap-1 whitespace-nowrap')
  })

  it('colors waypoint left borders by flight mode, extends to current leg, and uses muted neutral on origin', () => {
    const originRowHtml = renderRowItem({
      rowView: makeWaypointRow({ number: 1, visitState: 'visited' }),
      index: 0,
      lastVisitedIndex: 1,
    })
    expect(originRowHtml).toContain('style="border-left-color:#3d2816"')
    expect(originRowHtml).toContain(
      'border-l-2 shadow-[inset_0_-1px_0_0_var(--color-ed-border-subtle)]',
    )

    const currentLegRowHtml = renderRowItem({
      rowView: makeWaypointRow({
        row: { id: 3, systemName: 'Sagittarius A*' },
        number: 3,
        visitState: 'current',
        legModeId: 10,
        legModeColor: '#00E5A3',
      }),
      index: 2,
      isNextWaypoint: true,
      lastVisitedIndex: 1,
    })
    expect(currentLegRowHtml).toContain('style="border-left-color:#00E5A3"')
    expect(currentLegRowHtml).toContain(
      'border-l-2 shadow-[inset_0_-1px_0_0_var(--color-ed-border-subtle)]',
    )

    const futureRowHtml = renderRowItem({
      rowView: makeWaypointRow({
        row: { id: 4, systemName: 'Beagle Point' },
        number: 4,
        visitState: 'future',
        legModeColor: '#00E5A3',
      }),
      index: 3,
      lastVisitedIndex: 1,
    })
    expect(futureRowHtml).not.toContain('border-l-2')
  })

  it('supports collapsible columns: hides notes or carrier stop when toggled off', () => {
    const htmlHidden = renderRowItem({
      rowView: makeWaypointRow({
        row: {
          id: 99,
          systemName: 'Col 285 Sector',
          poiName: 'Planet 1',
          notes: 'Secret mining location',
        },
        number: 3,
        visitState: 'future',
        permissions: { hasDrawer: false },
      }),
      index: 2,
      isPlanning: true,
      showNotes: false,
      showCarrierStop: false,
    })

    expect(htmlHidden).not.toContain('Secret mining location')
    expect(htmlHidden).not.toContain('+ FC STOP')
    expect(htmlHidden).toContain('Col 285 Sector')
  })

  it('renders distance from player on active next waypoint and distance from previous waypoint in planning', () => {
    const activeNextHtml = renderRowItem({
      rowView: makeWaypointRow({
        row: { id: 18, systemName: 'Maia', poiName: 'Black Hole' },
        number: 2,
        visitState: 'current',
        distFromPrevLy: 380.5,
        distFromCurrentLy: 42.1,
        isNextWaypoint: true,
        permissions: { hasDrawer: true },
      }),
      isPlanning: false,
      isNextWaypoint: true,
      playerSystem: 'Merope',
    })
    expect(activeNextHtml).toContain('42.1 Ly')
    expect(activeNextHtml).not.toContain('380.5 Ly')
    expect(activeNextHtml).toContain('Estimated straight-line distance from player location (Merope)')
    expect(activeNextHtml).toContain('aria-label="Toggle per-waypoint leg telemetry drawer"')

    const planningHtml = renderRowItem({
      rowView: makeWaypointRow({
        row: { id: 19, systemName: 'Maia' },
        number: 2,
        visitState: 'future',
        distFromPrevLy: 380.5,
        distFromCurrentLy: 42.1,
        isNextWaypoint: true,
        permissions: { hasDrawer: false },
      }),
      isPlanning: true,
      isNextWaypoint: true,
      playerSystem: 'Merope',
    })
    expect(planningHtml).toContain('380.5 Ly')
    expect(planningHtml).not.toContain('42.1 Ly')
    expect(planningHtml).toContain('Estimated straight-line distance from previous waypoint')
  })

  it('renders CoordStatusIndicator tooltip, search highlight attribute, and per-row copy feedback', () => {
    const edsmRowHtml = renderRowItem({
      rowView: makeWaypointRow({
        row: {
          id: 30,
          systemName: 'Beagle Point',
          hasCoords: true,
          coordsSource: 'edsm',
          x: -1111.6,
          y: -134.2,
          z: 65269.8,
        },
        number: 1,
        detectedRegion: 'The Abyss',
        visitCount: 0,
        visitState: 'future',
      }),
      isSearchHighlighted: true,
      isSystemCopied: false,
    })
    expect(edsmRowHtml).toContain(
      'title="The Abyss (-1111.6, -134.2, 65269.8) via EDSM (Never Visited)"',
    )
    expect(edsmRowHtml).toContain('data-search-highlighted="true"')
    expect(edsmRowHtml).toMatch(/Copy [^"]*Beagle Point[^"]* to clipboard/)

    const draggedCopiedHtml = renderRowItem({
      rowView: makeWaypointRow({
        row: {
          id: 31,
          systemName: 'Sol',
          hasCoords: true,
          coordsSource: 'journal',
          x: 0,
          y: 0,
          z: 0,
        },
        number: 2,
        detectedRegion: 'Inner Orion Spur',
        visitCount: 3,
        visitState: 'visited',
      }),
      isBeingDragged: true,
      isSearchHighlighted: true,
      isSystemCopied: true,
    })
    expect(draggedCopiedHtml).toContain('title="Inner Orion Spur (0.0, 0.0, 0.0) via Journal (3 Visits)"')
    expect(draggedCopiedHtml).not.toContain('data-search-highlighted="true"')
    expect(draggedCopiedHtml).toMatch(/Copied [^"]*Sol[^"]*!/)
    expect(draggedCopiedHtml).toContain('text-ed-cyan')
  })

  it('locks star system on visited rows while keeping manipulation controls on N > 1 and locking Origin #1', () => {
    const visitedSecondHtml = renderRowItem({
      rowView: makeWaypointRow({
        row: { id: 41, systemName: 'Alpha Centauri', visitedAt: '3312-01-10T10:00:00Z' },
        number: 2,
        visitState: 'visited',
        visitedAtGalactic: '3312-01-10 10:00 UTC',
        allowInsertWaypoint: false,
        permissions: { allowInsertWaypoint: false },
      }),
      index: 1,
      lastVisitedIndex: 2,
      insertMenuOpen: true,
    })

    expect(visitedSecondHtml).not.toContain('placeholder="Star System"')
    expect(visitedSecondHtml).toContain('title="Insert row below"')
    expect(visitedSecondHtml).toContain(
      'title="Hold to preview valid drop positions, drag to reorder waypoint"',
    )
    expect(visitedSecondHtml).toContain('title="Delete row"')
    expect(visitedSecondHtml).toContain('+ Insert Region Header Below')
    expect(visitedSecondHtml).toContain(
      'Add new waypoints in the unvisited section and drag them into valid past slots.',
    )

    const originHtml = renderRowItem({
      rowView: makeWaypointRow({
        row: { id: 40, visitedAt: '3312-01-01T00:00:00Z' },
        number: 1,
        visitState: 'visited',
        visitedAtGalactic: '3312-01-01 00:00 UTC',
      }),
      index: 0,
      lastVisitedIndex: 2,
    })
    expect(originHtml).toContain('disabled="" title="Origin waypoint cannot be deleted"')
    expect(originHtml).toContain(
      'disabled="" draggable="false" title="Origin waypoint cannot be reordered"',
    )
  })

  it('renders unvisited gap line highlights during hold and drag and origin DEPARTED / AWAITING DEPARTURE labels', () => {
    const baseRow = makeWaypointRow({
      row: { id: 50, systemName: 'Rohini' },
      number: 3,
      visitState: 'future',
      distFromPrevLy: 120.0,
    })
    expect(renderRowItem({ rowView: baseRow, topGapHighlight: true })).toContain(
      '*:shadow-[inset_0_2px_0_0_var(--color-ed-cyan)]',
    )
    expect(renderRowItem({ rowView: baseRow, bottomGapHighlight: true })).toContain(
      '*:shadow-[inset_0_-2px_0_0_var(--color-ed-cyan)]',
    )
    const grabbedHtml = renderRowItem({ rowView: baseRow, isBeingDragged: true })
    expect(grabbedHtml).toContain('data-being-dragged="true"')
    expect(grabbedHtml).toContain('MOVING')

    const stagedOriginHtml = renderRowItem({
      rowView: makeWaypointRow({
        row: { id: 60, visitedAt: '2026-01-18T10:00:00Z', departedAt: '' },
        number: 1,
        visitState: 'visited',
        visitedAtGalactic: '3312-01-18 10:00 UTC',
      }),
      index: 0,
      lastVisitedIndex: 0,
    })
    expect(stagedOriginHtml).toContain('AWAITING DEPARTURE')
    expect(stagedOriginHtml).not.toContain('LOGGED VISIT')

    const departedOriginHtml = renderRowItem({
      rowView: makeWaypointRow({
        row: {
          id: 61,
          visitedAt: '2026-01-18T10:00:00Z',
          departedAt: '2026-01-18T12:00:00Z',
        },
        number: 1,
        visitState: 'visited',
        visitedAtGalactic: '3312-01-18 10:00 UTC',
        departedAtGalactic: '3312-01-18 12:00 UTC',
      }),
      index: 0,
      lastVisitedIndex: 1,
    })
    expect(departedOriginHtml).toContain('DEPARTED')
    expect(departedOriginHtml).toContain('3312-01-18 12:00 UTC')

    const visitedDestinationHtml = renderRowItem({
      rowView: makeWaypointRow({
        row: {
          id: 62,
          systemName: 'Alpha Centauri',
          visitedAt: '2026-01-18T13:00:00Z',
          departedAt: '2026-01-18T14:30:00Z',
        },
        number: 2,
        visitState: 'visited',
        visitedAtGalactic: '3312-01-18 13:00 UTC',
        departedAtGalactic: '3312-01-18 14:30 UTC',
      }),
      index: 1,
      lastVisitedIndex: 1,
    })
    expect(visitedDestinationHtml).toContain('LOGGED VISIT')
    expect(visitedDestinationHtml).toContain('3312-01-18 13:00 UTC')
  })
})

describe('WaypointRowItem lock transition, rollback draft recovery, and visit inspection', () => {
  function mountRows(
    root: ReturnType<typeof createRoot>,
    ...rows: Array<Partial<Parameters<typeof WaypointRowItem>[0]> & { rowView: expedition.RowView }>
  ) {
    act(() => {
      root.render(
        createElement(
          'table',
          null,
          createElement(
            'tbody',
            null,
            ...rows.map((itemProps, itemIndex) =>
              createElement(WaypointRowItem, {
                key: itemProps.rowView.row.id ?? itemIndex,
                index: itemIndex,
                isPlanning: false,
                isLocked: false,
                isBeingDragged: false,
                isExpanded: false,
                isSystemCopied: false,
                insertMenuOpen: false,
                lastVisitedIndex: 0,
                rowStyle: {},
                dropProps: {},
                handlers: defaultRowHandlers,
                ...itemProps,
              }),
            ),
          ),
        ),
      )
    })
  }

  it('clears editing state when a focused system input becomes locked so subsequent poiName updates sync into draft', () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    const renderMountedRow = (rowView: expedition.RowView, lastVisitedIndex: number) =>
      mountRows(root, { rowView, index: 1, lastVisitedIndex })

    try {
      renderMountedRow(
        makeWaypointRow({
          row: { id: 70, systemName: 'Colonia', poiName: '' },
          number: 2,
          visitState: 'future',
        }),
        0,
      )

      const systemInput = container.querySelector(
        'input[placeholder="Star System"]',
      ) as HTMLInputElement
      act(() => {
        systemInput.focus()
        setFormControlValue(systemInput, 'Colonia Draft')
      })
      expect(systemInput.value).toBe('Colonia Draft')

      renderMountedRow(
        makeWaypointRow({
          row: {
            id: 70,
            systemName: 'Colonia',
            poiName: '',
            visitedAt: '3312-02-01T10:00:00Z',
          },
          number: 2,
          visitState: 'visited',
          visitedAtGalactic: '3312-02-01 10:00 UTC',
        }),
        1,
      )
      expect(container.querySelector('input[placeholder="Star System"]')).toBeNull()

      renderMountedRow(
        makeWaypointRow({
          row: {
            id: 70,
            systemName: 'Colonia',
            poiName: 'Jaques Station',
            visitedAt: '3312-02-01T10:00:00Z',
          },
          number: 2,
          visitState: 'visited',
          visitedAtGalactic: '3312-02-01 10:00 UTC',
        }),
        1,
      )

      const poiTextarea = container.querySelector(
        'textarea[placeholder="POI Name"]',
      ) as HTMLTextAreaElement
      expect(poiTextarea.value).toBe('Jaques Station')
    } finally {
      act(() => root.unmount())
      container.remove()
    }
  })

  it('restores rejected systemName draft without overwriting a newly focused poiName edit', async () => {
    let resolveSystemBlur!: (succeeded: boolean) => void
    const onFieldBlur = vi.fn(
      () =>
        new Promise<boolean>((resolve) => {
          resolveSystemBlur = resolve
        }),
    )
    const rowView = makeWaypointRow({
      row: { id: 71, systemName: 'Sagittarius A*', poiName: 'Supermassive Black Hole' },
      number: 3,
      visitState: 'future',
    })

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    try {
      mountRows(root, {
        rowView,
        index: 2,
        isPlanning: true,
        handlers: { ...defaultRowHandlers, onFieldBlur },
      })

      const systemInput = container.querySelector(
        'input[placeholder="Star System"]',
      ) as HTMLInputElement
      const poiTextarea = container.querySelector(
        'textarea[placeholder="POI Name"]',
      ) as HTMLTextAreaElement
      act(() => {
        systemInput.focus()
        setFormControlValue(systemInput, 'Rejected System Name')
      })

      act(() => {
        systemInput.blur()
        poiTextarea.focus()
        setFormControlValue(poiTextarea, 'Explorer Anchorage')
      })
      expect(onFieldBlur).toHaveBeenCalledWith(rowView.row, 'systemName', 'Rejected System Name')

      await act(async () => {
        resolveSystemBlur(false)
        await Promise.resolve()
      })

      expect(systemInput.value).toBe('Sagittarius A*')
      expect(poiTextarea.value).toBe('Explorer Anchorage')
    } finally {
      act(() => root.unmount())
      container.remove()
    }
  })

  it('makes green journal CoordStatusIndicator clickable with two-line tooltip while leaving EDSM non-clickable', () => {
    const onInspectSystemVisits = vi.fn()
    const handlers: WaypointRowHandlers = { ...defaultRowHandlers, onInspectSystemVisits }
    const journalRowView = makeWaypointRow({
      row: { id: 72, systemName: 'Sol', hasCoords: true, coordsSource: 'journal', x: 0, y: 0, z: 0 },
      number: 1,
      detectedRegion: 'Inner Orion Spur',
      visitCount: 3,
      visitState: 'visited',
    })
    const edsmRowView = makeWaypointRow({
      row: {
        id: 73,
        systemName: 'Beagle Point',
        hasCoords: true,
        coordsSource: 'edsm',
        x: -1111.6,
        y: -134.2,
        z: 65269.8,
      },
      number: 2,
      detectedRegion: 'The Abyss',
      visitCount: 0,
      visitState: 'future',
    })

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    try {
      mountRows(
        root,
        { rowView: journalRowView, index: 0, handlers },
        { rowView: edsmRowView, index: 1, handlers },
      )

      const journalButton = container.querySelector(
        'button[aria-label="Search visit history for Sol"]',
      ) as HTMLButtonElement | null
      expect(journalButton).toBeTruthy()
      expect(journalButton?.title).toBe(
        'Inner Orion Spur (0.0, 0.0, 0.0) via Journal (3 Visits)\nClick to search visit history',
      )

      act(() => {
        journalButton?.click()
      })
      expect(onInspectSystemVisits).toHaveBeenCalledTimes(1)
      expect(onInspectSystemVisits).toHaveBeenCalledWith('Sol')

      expect(
        container.querySelector('button[aria-label="Search visit history for Beagle Point"]'),
      ).toBeNull()
      expect(
        container.querySelector(
          'span[title="The Abyss (-1111.6, -134.2, 65269.8) via EDSM (Never Visited)"]',
        ),
      ).toBeTruthy()
    } finally {
      act(() => root.unmount())
      container.remove()
    }
  })
})
