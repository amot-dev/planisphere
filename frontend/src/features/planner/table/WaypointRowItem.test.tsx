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

describe('WaypointRowItem and AutoExpandingTextarea Row Height Consistency', () => {
  function renderRowItem(
    props: Partial<Parameters<typeof WaypointRowItem>[0]> & { rowView: expedition.RowView },
  ) {
    const defaultProps: Parameters<typeof WaypointRowItem>[0] = {
      index: 1,
      isPlanning: false,
      isLocked: false,
      isBeingDragged: false,
      isExpanded: false,
      isNextFcStop: false,
      hasActiveCarrierRoute: false,
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

  function createOriginRowView(
    overrides: Partial<Omit<expedition.RowView, 'row' | 'convertValues'>> & {
      row?: Partial<expedition.Row>
    } = {},
  ) {
    const { row: rowOverrides, ...rowViewOverrides } = overrides
    return expedition.RowView.createFrom({
      row: { id: 1, expeditionId: 1, rowType: 'waypoint', systemName: 'Sol', ...rowOverrides },
      number: 1,
      visitState: 'visited',
      ...rowViewOverrides,
    })
  }

  it('renders visited row with uniform h-7 (28px) elements, align-top cells, and inset shadow left border', () => {
    const mockRowView = expedition.RowView.createFrom({
      row: {
        id: 11,
        expeditionId: 1,
        rowType: 'waypoint',
        systemName: 'Sol',
        seq: 10,
        poiName: 'Earth',
        notes: 'Returning to Bubble to participate in event',
      },
      number: 11,
      visitState: 'visited',
      visitedAtGalactic: '3310-02-15 14:32',
    })

    const html = renderRowItem({
      rowView: mockRowView,
      index: 10,
      isLocked: true,
      lastVisitedIndex: 10,
    })

    // Cell padding py-1.5 and align-top on all cells
    expect(html).toContain('align-top')
    expect(html).toContain('py-1.5')

    // Chevron cell has border-l-2 and inset shadow bottom line so it never miters or clips the left border
    expect(html).toContain('style="border-left-color:#FF7100"')
    expect(html).toContain('border-l-2 shadow-[inset_0_-1px_0_0_var(--color-ed-border-subtle)]')

    // Uniform 28px (h-7) on chevron, waypoint #, star system, carrier stop, telemetry, and controls
    expect(html).toContain('h-7 flex items-center justify-center') // Chevron container
    expect(html).toContain(
      'h-7 flex items-center justify-end font-mono-tabular font-bold text-ed-amber text-xs leading-5',
    ) // Waypoint #
    expect(html).toContain(
      'w-full h-7 flex items-center font-mono-tabular font-bold text-ed-text-main px-1.5 py-[3px] border border-transparent select-text cursor-text truncate text-xs leading-5',
    ) // Star system locked
    expect(html).toContain('h-7 inline-flex items-center justify-center gap-1.5 whitespace-nowrap') // Carrier stop container
    expect(html).toContain(
      'h-7 flex items-center gap-1.5 whitespace-nowrap font-mono-tabular text-[10px] leading-5',
    ) // Telemetry container (visited)
    expect(html).toContain('h-7 inline-flex items-center justify-end gap-1 whitespace-nowrap') // Controls container

    // Textareas have min-h-[28px], py-[3px], and native rows="1" without artificial wrap
    expect(html).toContain('min-h-[28px]')
    expect(html).toContain('py-[3px]')
    expect(html).toContain('rows="1"')
    expect(html).not.toContain('rows="2"')
  })

  it('renders unvisited row with matching h-7 (28px) elements and input box model', () => {
    const mockRowView = expedition.RowView.createFrom({
      row: {
        id: 17,
        expeditionId: 1,
        rowType: 'waypoint',
        systemName: 'LFT 509',
        seq: 16,
        poiName: 'Outpost',
        notes: '',
      },
      number: 17,
      visitState: 'future',
      distFromPrevLy: 42.5,
    })

    const html = renderRowItem({
      rowView: mockRowView,
      index: 16,
      lastVisitedIndex: 10,
    })

    // Unvisited input has h-7, py-[3px], border-transparent matching the locked span
    expect(html).toContain('w-full h-7 bg-transparent px-1.5 py-[3px] border border-transparent')
    // Telemetry for unvisited has h-7 flex items-center
    expect(html).toContain('h-7 flex items-center whitespace-nowrap font-mono-tabular text-[11px]')
    // Controls for unvisited has h-7 inline-flex items-center
    expect(html).toContain('h-7 inline-flex items-center justify-end gap-1 whitespace-nowrap')
  })

  it('colors waypoint left borders by flight mode, extends to current leg, and uses muted neutral on origin', () => {
    const originRowView = createOriginRowView()
    const originRowHtml = renderRowItem({ rowView: originRowView, index: 0, lastVisitedIndex: 1 })
    expect(originRowHtml).toContain('style="border-left-color:#3d2816"')
    expect(originRowHtml).toContain(
      'border-l-2 shadow-[inset_0_-1px_0_0_var(--color-ed-border-subtle)]',
    )

    const currentLegRowView = expedition.RowView.createFrom({
      row: { id: 3, expeditionId: 1, rowType: 'waypoint', systemName: 'Sagittarius A*' },
      number: 3,
      visitState: 'current',
      legModeId: 10,
      legModeColor: '#00E5A3',
    })
    const currentLegRowHtml = renderRowItem({
      rowView: currentLegRowView,
      index: 2,
      isNextWaypoint: true,
      lastVisitedIndex: 1,
    })
    expect(currentLegRowHtml).toContain('style="border-left-color:#00E5A3"')
    expect(currentLegRowHtml).toContain(
      'border-l-2 shadow-[inset_0_-1px_0_0_var(--color-ed-border-subtle)]',
    )

    const futureRowView = expedition.RowView.createFrom({
      row: { id: 4, expeditionId: 1, rowType: 'waypoint', systemName: 'Beagle Point' },
      number: 4,
      visitState: 'future',
      legModeColor: '#00E5A3',
    })
    const futureRowHtml = renderRowItem({ rowView: futureRowView, index: 3, lastVisitedIndex: 1 })
    expect(futureRowHtml).not.toContain('border-l-2')
  })

  it('supports collapsible columns: hides notes or carrier stop when toggled off', () => {
    const mockRowView = expedition.RowView.createFrom({
      row: {
        id: 99,
        expeditionId: 1,
        rowType: 'waypoint',
        systemName: 'Col 285 Sector',
        poiName: 'Planet 1',
        notes: 'Secret mining location',
      },
      number: 3,
      visitState: 'future',
    })

    const htmlHidden = renderRowItem({
      rowView: mockRowView,
      index: 2,
      isPlanning: true,
      showNotes: false,
      showCarrierStop: false,
    })

    // Secret mining location and FC Stop should not be present
    expect(htmlHidden).not.toContain('Secret mining location')
    expect(htmlHidden).not.toContain('+ FC STOP')
    expect(htmlHidden).toContain('Col 285 Sector')
  })

  it('renders distance from player when row is next waypoint in active expedition', () => {
    const mockRowView = expedition.RowView.createFrom({
      row: {
        id: 18,
        expeditionId: 1,
        rowType: 'waypoint',
        systemName: 'Maia',
        seq: 2,
        poiName: 'Black Hole',
        notes: '',
      },
      number: 2,
      visitState: 'current',
      distFromPrevLy: 380.5,
      distFromCurrentLy: 42.1,
      isNextWaypoint: true,
    })

    const html = renderRowItem({
      rowView: mockRowView,
      isPlanning: false,
      isNextWaypoint: true,
      playerSystem: 'Merope',
    })

    // Should display distance from player (42.1 Ly) with player location tooltip, not 380.5 Ly, and render expand chevron
    expect(html).toContain('42.1 Ly')
    expect(html).not.toContain('380.5 Ly')
    expect(html).toContain('Estimated straight-line distance from player location (Merope)')
    expect(html).toContain('aria-label="Toggle per-waypoint leg telemetry drawer"')
    expect(html).not.toContain('title="Toggle per-waypoint leg telemetry drawer"')
  })

  it('renders distance from previous waypoint in planning status even if next waypoint', () => {
    const mockRowView = expedition.RowView.createFrom({
      row: {
        id: 19,
        expeditionId: 1,
        rowType: 'waypoint',
        systemName: 'Maia',
        seq: 2,
        poiName: '',
        notes: '',
      },
      number: 2,
      visitState: 'future',
      distFromPrevLy: 380.5,
      distFromCurrentLy: 42.1,
      isNextWaypoint: true,
    })

    const html = renderRowItem({
      rowView: mockRowView,
      isPlanning: true,
      isNextWaypoint: true,
      playerSystem: 'Merope',
    })

    // In planning mode, should display distance from previous waypoint (380.5 Ly)
    expect(html).toContain('380.5 Ly')
    expect(html).not.toContain('42.1 Ly')
    expect(html).toContain('Estimated straight-line distance from previous waypoint')
  })

  it('renders CoordStatusIndicator tooltip with region, coordinates, and EDSM or Journal visit count', () => {
    const edsmRowHtml = renderRowItem({
      rowView: expedition.RowView.createFrom({
        row: {
          id: 30,
          expeditionId: 1,
          rowType: 'waypoint',
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
    })
    expect(edsmRowHtml).toContain(
      'title="The Abyss (-1111.6, -134.2, 65269.8) via EDSM (Never Visited)"',
    )

    const journalRowHtml = renderRowItem({
      rowView: expedition.RowView.createFrom({
        row: {
          id: 31,
          expeditionId: 1,
          rowType: 'waypoint',
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
    })
    expect(journalRowHtml).toContain('title="Inner Orion Spur (0.0, 0.0, 0.0) via Journal (3 Visits)"')
  })

  it('applies search highlight styling and data attribute only when highlighted and not dragged', () => {
    const rowView = createOriginRowView({ row: { id: 89, systemName: 'Colonia' } })

    const highlightedHtml = renderRowItem({ rowView, isSearchHighlighted: true })
    expect(highlightedHtml).toContain('data-search-highlighted="true"')
    expect(highlightedHtml).toContain(
      'bg-ed-amber-tint *:bg-ed-amber-tint *:shadow-[inset_0_2px_0_0_var(--color-ed-cyan),inset_0_-2px_0_0_var(--color-ed-cyan)]',
    )

    const draggedAndHighlightedHtml = renderRowItem({
      rowView,
      isBeingDragged: true,
      isSearchHighlighted: true,
    })
    expect(draggedAndHighlightedHtml).not.toContain('data-search-highlighted="true"')
  })

  it('locks star system on visited rows while keeping manipulation controls on N > 1 and locking Origin #1', () => {
    const visitedSecondHtml = renderRowItem({
      rowView: expedition.RowView.createFrom({
        row: {
          id: 41,
          expeditionId: 1,
          rowType: 'waypoint',
          systemName: 'Alpha Centauri',
          visitedAt: '3312-01-10T10:00:00Z',
        },
        number: 2,
        visitState: 'visited',
        visitedAtGalactic: '3312-01-10 10:00 UTC',
      }),
      index: 1,
      isLocked: false,
      lastVisitedIndex: 2,
      insertMenuOpen: true,
    })

    // System name is locked (rendered as span, not input)
    expect(visitedSecondHtml).not.toContain('placeholder="Star System"')
    // Row manipulation buttons (+, drag handle, delete) are present, while up/down buttons are absent
    expect(visitedSecondHtml).toContain('title="Insert row below"')
    expect(visitedSecondHtml).toContain(
      'title="Hold to preview valid drop positions, drag to reorder waypoint"',
    )
    expect(visitedSecondHtml).toContain('title="Delete row"')
    expect(visitedSecondHtml).not.toContain('↑')
    expect(visitedSecondHtml).not.toContain('↓')
    // Insert menu before lastVisitedIndex allows region headers and shows waypoint hint
    expect(visitedSecondHtml).toContain('+ Insert Region Header Below')
    expect(visitedSecondHtml).toContain(
      'Add new waypoints in the unvisited section and drag them into valid past slots.',
    )

    const originHtml = renderRowItem({
      rowView: createOriginRowView({
        row: { id: 40, visitedAt: '3312-01-01T00:00:00Z' },
        visitedAtGalactic: '3312-01-01 00:00 UTC',
      }),
      index: 0,
      isLocked: false,
      lastVisitedIndex: 2,
    })

    // Origin #1 disables drag and delete controls with explanatory tooltips to preserve alignment
    expect(originHtml).toContain('title="Insert row below"')
    expect(originHtml).toContain('disabled="" title="Origin waypoint cannot be deleted"')
    expect(originHtml).toContain(
      'disabled="" draggable="false" title="Origin waypoint cannot be reordered"',
    )
  })

  it('renders unvisited gap line highlights during hold and drag', () => {
    const baseRow = expedition.RowView.createFrom({
      row: {
        id: 50,
        expeditionId: 1,
        rowType: 'waypoint',
        systemName: 'Rohini',
      },
      number: 3,
      visitState: 'future',
      distFromPrevLy: 120.0,
    })

    // Unvisited row preserves normal distance telemetry and shows top/bottom gap lines when hovered
    const topGapRowHtml = renderRowItem({
      rowView: baseRow,
      topGapHighlight: true,
    })
    expect(topGapRowHtml).toContain('*:shadow-[inset_0_2px_0_0_var(--color-ed-cyan)]')
    expect(topGapRowHtml).toContain('120.0 Ly')
    expect(topGapRowHtml).not.toContain('UNVISITED SLOT')

    const bottomGapRowHtml = renderRowItem({
      rowView: baseRow,
      bottomGapHighlight: true,
    })
    expect(bottomGapRowHtml).toContain('*:shadow-[inset_0_-2px_0_0_var(--color-ed-cyan)]')

    const grabbedRowHtml = renderRowItem({
      rowView: baseRow,
      isBeingDragged: true,
    })
    expect(grabbedRowHtml).toContain('data-being-dragged="true"')
    expect(grabbedRowHtml).toContain('MOVING')
    expect(grabbedRowHtml).toContain(
      '*:shadow-[inset_0_2px_0_0_var(--color-ed-amber),inset_0_-2px_0_0_var(--color-ed-amber)]',
    )
  })

  it('toggles copy feedback styling and tooltip strictly from per-row isSystemCopied', () => {
    const rowView = expedition.RowView.createFrom({
      row: { id: 88, expeditionId: 1, rowType: 'waypoint', systemName: 'Sagittarius A*' },
      number: 4,
      visitState: 'future',
    })

    const uncopiedHtml = renderRowItem({ rowView, isSystemCopied: false })
    expect(uncopiedHtml).toMatch(/Copy [^"]*Sagittarius A\*[^"]* to clipboard/)
    expect(uncopiedHtml).not.toMatch(/Copied [^"]*Sagittarius A\*[^"]*!/)

    const copiedHtml = renderRowItem({ rowView, isSystemCopied: true })
    expect(copiedHtml).toMatch(/Copied [^"]*Sagittarius A\*[^"]*!/)
    expect(copiedHtml).toContain('text-ed-cyan')
  })

  it('renders DEPARTED and AWAITING DEPARTURE on origin #1 while preserving LOGGED VISIT on waypoints > 1', () => {
    const stagedOriginHtml = renderRowItem({
      rowView: createOriginRowView({
        row: { id: 60, visitedAt: '2026-01-18T10:00:00Z', departedAt: '' },
        visitedAtGalactic: '3312-01-18 10:00 UTC',
        departedAtGalactic: '',
      }),
      index: 0,
      lastVisitedIndex: 0,
    })
    expect(stagedOriginHtml).toContain('AWAITING DEPARTURE')
    expect(stagedOriginHtml).not.toContain('LOGGED VISIT')
    expect(stagedOriginHtml).not.toContain('Staged at Waypoint #1')
    expect(stagedOriginHtml).not.toContain('Awaiting departure')

    const departedOriginHtml = renderRowItem({
      rowView: createOriginRowView({
        row: {
          id: 61,
          visitedAt: '2026-01-18T10:00:00Z',
          departedAt: '2026-01-18T12:00:00Z',
        },
        visitedAtGalactic: '3312-01-18 10:00 UTC',
        departedAtGalactic: '3312-01-18 12:00 UTC',
      }),
      index: 0,
      lastVisitedIndex: 1,
    })
    expect(departedOriginHtml).toContain('DEPARTED')
    expect(departedOriginHtml).toContain('3312-01-18 12:00 UTC')
    expect(departedOriginHtml).not.toContain('LOGGED VISIT')
    expect(departedOriginHtml).not.toContain('title="Arrived ')
    expect(departedOriginHtml).not.toContain('title="Departed ')

    const visitedDestinationHtml = renderRowItem({
      rowView: expedition.RowView.createFrom({
        row: {
          id: 62,
          expeditionId: 1,
          rowType: 'waypoint',
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
    expect(visitedDestinationHtml).not.toContain('title="Arrived ')
  })
})

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

describe('WaypointRowItem lock transition and rollback draft recovery', () => {
  it('clears editing state when a focused system input becomes locked so subsequent background poiName updates sync into draft', () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    const renderMountedRow = (rowView: expedition.RowView, lastVisitedIndex: number) => {
      act(() => {
        root.render(
          createElement(
            'table',
            null,
            createElement(
              'tbody',
              null,
              createElement(WaypointRowItem, {
                rowView,
                index: 1,
                isPlanning: false,
                isLocked: false,
                isBeingDragged: false,
                isExpanded: false,
                isNextFcStop: false,
                hasActiveCarrierRoute: false,
                isSystemCopied: false,
                insertMenuOpen: false,
                lastVisitedIndex,
                rowStyle: {},
                dropProps: {},
                handlers: defaultRowHandlers,
              }),
            ),
          ),
        )
      })
    }

    try {
      renderMountedRow(
        expedition.RowView.createFrom({
          row: { id: 70, expeditionId: 1, rowType: 'waypoint', systemName: 'Colonia', poiName: '' },
          number: 2,
          visitState: 'future',
        }),
        0,
      )

      const systemInput = container.querySelector(
        'input[placeholder="Star System"]',
      ) as HTMLInputElement
      expect(systemInput).toBeTruthy()

      act(() => {
        systemInput.focus()
        setFormControlValue(systemInput, 'Colonia Draft')
      })
      expect(systemInput.value).toBe('Colonia Draft')

      // Live arrival locks the system cell while the input was focused.
      renderMountedRow(
        expedition.RowView.createFrom({
          row: {
            id: 70,
            expeditionId: 1,
            rowType: 'waypoint',
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

      // Subsequent background GEC resolution updates poiName on the locked row.
      renderMountedRow(
        expedition.RowView.createFrom({
          row: {
            id: 70,
            expeditionId: 1,
            rowType: 'waypoint',
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
    const handlers: WaypointRowHandlers = {
      ...defaultRowHandlers,
      onFieldBlur,
    }

    const rowView = expedition.RowView.createFrom({
      row: {
        id: 71,
        expeditionId: 1,
        rowType: 'waypoint',
        systemName: 'Sagittarius A*',
        poiName: 'Supermassive Black Hole',
      },
      number: 3,
      visitState: 'future',
    })

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    try {
      act(() => {
        root.render(
          createElement(
            'table',
            null,
            createElement(
              'tbody',
              null,
              createElement(WaypointRowItem, {
                rowView,
                index: 2,
                isPlanning: true,
                isLocked: false,
                isBeingDragged: false,
                isExpanded: false,
                isNextFcStop: false,
                hasActiveCarrierRoute: false,
                isSystemCopied: false,
                insertMenuOpen: false,
                lastVisitedIndex: -1,
                rowStyle: {},
                dropProps: {},
                handlers,
              }),
            ),
          ),
        )
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
      expect(systemInput.value).toBe('Rejected System Name')

      // Simulate Tab-blurring from Star System directly into POI Name before onFieldBlur settles.
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

  it('makes green journal CoordStatusIndicator clickable with two-line tooltip while leaving EDSM indicator non-clickable', () => {
    const onInspectSystemVisits = vi.fn()
    const handlers: WaypointRowHandlers = {
      ...defaultRowHandlers,
      onInspectSystemVisits,
    }
    const journalRowView = expedition.RowView.createFrom({
      row: {
        id: 72,
        expeditionId: 1,
        rowType: 'waypoint',
        systemName: 'Sol',
        hasCoords: true,
        coordsSource: 'journal',
        x: 0,
        y: 0,
        z: 0,
      },
      number: 1,
      detectedRegion: 'Inner Orion Spur',
      visitCount: 3,
      visitState: 'visited',
    })
    const edsmRowView = expedition.RowView.createFrom({
      row: {
        id: 73,
        expeditionId: 1,
        rowType: 'waypoint',
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
      act(() => {
        root.render(
          createElement(
            'table',
            null,
            createElement(
              'tbody',
              null,
              createElement(WaypointRowItem, {
                rowView: journalRowView,
                index: 0,
                isPlanning: false,
                isLocked: false,
                isBeingDragged: false,
                isExpanded: false,
                isNextFcStop: false,
                hasActiveCarrierRoute: false,
                isSystemCopied: false,
                insertMenuOpen: false,
                lastVisitedIndex: 0,
                rowStyle: {},
                dropProps: {},
                handlers,
              }),
              createElement(WaypointRowItem, {
                rowView: edsmRowView,
                index: 1,
                isPlanning: false,
                isLocked: false,
                isBeingDragged: false,
                isExpanded: false,
                isNextFcStop: false,
                hasActiveCarrierRoute: false,
                isSystemCopied: false,
                insertMenuOpen: false,
                lastVisitedIndex: 0,
                rowStyle: {},
                dropProps: {},
                handlers,
              }),
            ),
          ),
        )
      })

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
      const edsmIndicator = container.querySelector(
        'span[title="The Abyss (-1111.6, -134.2, 65269.8) via EDSM (Never Visited)"]',
      )
      expect(edsmIndicator).toBeTruthy()
    } finally {
      act(() => root.unmount())
      container.remove()
    }
  })
})
