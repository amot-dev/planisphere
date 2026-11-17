import { describe, expect, it } from 'vitest'
import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { WaypointRowItem } from './WaypointRowItem'
import { WaypointDetailsDrawer } from './WaypointDetailsDrawer'
import { PlannerCollapsedVisitedRow, PlannerDropSlotRow } from './PlannerDragPreviewRows'
import { expedition } from '../../../wailsjs/go/models'

describe('WaypointRowItem and AutoExpandingTextarea Row Height Consistency', () => {
  function renderRowItem(props: Partial<Parameters<typeof WaypointRowItem>[0]> & { rowView: expedition.RowView }) {
    const defaultProps: Parameters<typeof WaypointRowItem>[0] = {
      index: 1,
      isPlanning: false,
      isLocked: false,
      isBeingDragged: false,
      isExpanded: false,
      isNextFcStop: false,
      hasActiveCarrierRoute: false,
      copiedSystemRowId: null,
      insertMenuOpen: false,
      lastVisitedIndex: 0,
      rowStyle: {},
      dropProps: {},
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
      ...props,
    }
    return renderToString(
      createElement('table', null,
        createElement('tbody', null,
          createElement(WaypointRowItem, defaultProps),
        ),
      ),
    )
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
    expect(html).toContain('border-l-2 border-l-[#FF7100] shadow-[inset_0_-1px_0_0_#24180e]')

    // Uniform 28px (h-7) on chevron, waypoint #, star system, carrier stop, telemetry, and controls
    expect(html).toContain('h-7 flex items-center justify-center') // Chevron container
    expect(html).toContain('h-7 flex items-center justify-end font-mono-tabular font-bold text-[#FF7100] text-xs leading-5') // Waypoint #
    expect(html).toContain('w-full h-7 flex items-center font-mono-tabular font-bold text-[#F4ECE4] px-1.5 py-[3px] border border-transparent select-text cursor-text truncate text-xs leading-5') // Star system locked
    expect(html).toContain('h-7 inline-flex items-center justify-center gap-1.5 whitespace-nowrap') // Carrier stop container
    expect(html).toContain('h-7 flex items-center gap-1.5 whitespace-nowrap font-mono-tabular text-[10px] leading-5') // Telemetry container (visited)
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

  it('renders carrier stop elements with uniform h-5 height inside h-7 container for both active stop and add button', () => {
    const fcStopRowView = expedition.RowView.createFrom({
      row: {
        id: 20,
        expeditionId: 1,
        rowType: 'waypoint',
        systemName: 'Colonia',
        seq: 19,
        fcStop: true,
        fcArrived: true,
      },
      number: 20,
      visitState: 'visited',
    })

    const html = renderRowItem({
      rowView: fcStopRowView,
      index: 19,
      isLocked: true,
      lastVisitedIndex: 20,
    })

    expect(html).toContain('ARRIVED')
    expect(html).toContain('h-5 px-1.5 inline-flex items-center justify-center')
    expect(html).toContain('h-7 inline-flex items-center justify-center gap-1.5 whitespace-nowrap')

    const lockedFcRowHtml = renderRowItem({
      rowView: expedition.RowView.createFrom({
        row: {
          id: 501,
          expeditionId: 1,
          rowType: 'waypoint',
          systemName: 'Rohini',
          fcStop: true,
          fcArrived: false,
        },
        number: 2,
        visitState: 'future',
      }),
      index: 1,
      isFcStopLocked: true,
      hasActiveCarrierRoute: true,
      lastVisitedIndex: 0,
    })

    expect(lockedFcRowHtml).toContain('ROUTING')
    expect(lockedFcRowHtml).not.toContain('Remove Fleet Carrier stop designation')
    expect(lockedFcRowHtml).not.toContain('+ FC STOP')
  })

  it('renders WaypointDetailsDrawer with matching border-l-2, Vessel Flown, and 1-Click Mode toggle for leg waypoints (number > 1)', () => {
    const mockRowView = expedition.RowView.createFrom({
      row: {
        id: 15,
        expeditionId: 1,
        rowType: 'waypoint',
        systemName: 'Alpha Centauri',
      },
      number: 2,
      visitState: 'visited',
      legModeId: 10,
      legModeName: 'Exploration',
      legModeColor: '#00D2FF',
      transitions: [
        { kind: 'ship', to: 'Anaconda' },
      ],
    })

    const mockDetails = expedition.WaypointDetails.createFrom({
      kind: 'historical',
      number: 2,
      metrics: {
        distanceLy: 120.5,
        shipDistanceLy: 120.5,
        jumps: 3,
      } as any,
    })

    const mockModes = [
      { id: 10, name: 'Exploration', color: '#00D2FF' } as any,
      { id: 20, name: 'Travel', color: '#FF7100' } as any,
    ]

    const html = renderToString(
      createElement(WaypointDetailsDrawer, {
        details: mockDetails,
        loading: false,
        rowView: mockRowView,
        modeId: 10,
        modeColor: '#00D2FF',
        modesList: mockModes,
        onChangeMode: () => {},
      }),
    )

    expect(html).toContain('border-l-2 border-l-[#FF7100] shadow-[inset_0_1px_0_0_#3d2816]')
    expect(html).not.toContain('border-l-3')
    expect(html).toContain('[VESSEL FLOWN]:')
    expect(html).toContain('Anaconda')
    expect(html).toContain('[FLIGHT MODE]:')
    expect(html).toContain('Exploration')
    expect(html).toContain('Flight mode for this leg (click to change)')
  })

  it('omits Vessel Flown and Mode toggle in WaypointDetailsDrawer for origin baseline waypoint #1', () => {
    const mockRowView = expedition.RowView.createFrom({
      row: {
        id: 1,
        expeditionId: 1,
        rowType: 'waypoint',
        systemName: 'Sol',
      },
      number: 1,
      visitState: 'visited',
    })

    const mockDetails = expedition.WaypointDetails.createFrom({
      kind: 'origin',
      number: 1,
      metrics: {} as any,
    })

    const html = renderToString(
      createElement(WaypointDetailsDrawer, {
        details: mockDetails,
        loading: false,
        rowView: mockRowView,
      }),
    )

    expect(html).toContain('HISTORICAL BASELINE')
    expect(html).not.toContain('[VESSEL FLOWN]:')
    expect(html).not.toContain('[FLIGHT MODE]:')
  })

  it('renders Realized Value card with combined total sold and 5 orange negative lines without emojis', () => {
    const mockRowView = expedition.RowView.createFrom({
      row: {
        id: 2,
        expeditionId: 1,
        rowType: 'waypoint',
        systemName: 'Colonia',
      },
      number: 2,
      visitState: 'visited',
    })

    const mockDetails = expedition.WaypointDetails.createFrom({
      kind: 'leg',
      number: 2,
      metrics: {
        scanValueTotal: 50000000,
        organicValueTotal: 95000000,
      } as any,
      realization: {
        totalRealized: 120000000,
        cartoLostDeath: 5000000,
        organicLostDeath: 10000000,
        cartoStolen: 2000000,
        organicStolen: 8000000,
        carrierTariffLoss: 3500000,
        npcCrewLoss: 1200000,
      } as any,
    })

    const html = renderToString(
      createElement(WaypointDetailsDrawer, {
        details: mockDetails,
        loading: false,
        rowView: mockRowView,
      }),
    )

    // Check Realized Value card header
    expect(html).toContain('Realized Value')
    expect(html).toContain('120,000,000 Cr')

    // Check the six orange negative deduction lines
    expect(html).toContain('Cartographics Lost (Death)')
    expect(html).toContain('-5,000,000 Cr')

    expect(html).toContain('Exobiology Lost (Death)')
    expect(html).toContain('-10,000,000 Cr')

    expect(html).toContain('Cartographics Stolen')
    expect(html).toContain('-2,000,000 Cr')

    expect(html).toContain('Exobiology Stolen')
    expect(html).toContain('-8,000,000 Cr')

    expect(html).toContain('Carrier Tariffs')
    expect(html).toContain('-3,500,000 Cr')

    expect(html).toContain('NPC Crew Payouts')
    expect(html).toContain('-1,200,000 Cr')

    // Ensure the header and all six deduction values have orange text-[#FF7100] styling
    const orangeMatches = html.match(/text-\[#FF7100\]/g)
    expect(orangeMatches && orangeMatches.length).toBeGreaterThanOrEqual(7)

    // Ensure strictly no emojis anywhere in the drawer HTML
    const emojiRegex = /[\u{1F300}-\u{1F9FF}\u{2600}-\u{26FF}\u{2700}-\u{27BF}]/u
    expect(emojiRegex.test(html)).toBe(false)
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
    expect(html).toContain('title="Toggle per-waypoint leg telemetry drawer"')
  })

  it('renders WaypointDetailsDrawer for active current leg with IN PROGRESS and TBD fallback', () => {
    const currentLegRowView = expedition.RowView.createFrom({
      row: {
        id: 22,
        expeditionId: 1,
        rowType: 'waypoint',
        systemName: '',
      },
      number: 3,
      visitState: 'current',
    })

    const currentLegDetails = expedition.WaypointDetails.createFrom({
      kind: 'leg',
      number: 3,
      prevSystem: 'Alpha Centauri',
      fromGalactic: '3312-01-19 12:00 UTC',
      toGalactic: '',
      elapsed: '4h 15m',
      metrics: {} as any,
    })

    const html = renderToString(
      createElement(WaypointDetailsDrawer, {
        details: currentLegDetails,
        loading: false,
        rowView: currentLegRowView,
      }),
    )

    expect(html).toContain('LEG #2 <span class="relative -top-px">→</span> #3 // DELTA TELEMETRY')
    expect(html).toContain(
      'Alpha Centauri <span class="relative -top-0.5">→</span> <span class="text-[#F4ECE4] font-bold">TBD</span>',
    )
    expect(html).toContain(
      '3312-01-19 12:00 UTC <span class="relative -top-0.5">→</span> <span class="text-[#00D2FF] font-bold">IN PROGRESS</span>',
    )
    expect(html).toContain('[4h 15m]')
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
    expect(edsmRowHtml).toContain('title="The Abyss (-1111.6, -134.2, 65269.8) via EDSM (Never Visited)"')

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
    expect(visitedSecondHtml).toContain('title="Hold to preview valid drop positions, drag to reorder waypoint"')
    expect(visitedSecondHtml).toContain('title="Delete row"')
    expect(visitedSecondHtml).not.toContain('↑')
    expect(visitedSecondHtml).not.toContain('↓')
    // Insert menu before lastVisitedIndex allows region headers and shows waypoint hint
    expect(visitedSecondHtml).toContain('+ Insert Region Header Below')
    expect(visitedSecondHtml).toContain('Add new waypoints in the unvisited section and drag them into valid past slots.')

    const originHtml = renderRowItem({
      rowView: expedition.RowView.createFrom({
        row: {
          id: 40,
          expeditionId: 1,
          rowType: 'waypoint',
          systemName: 'Sol',
          visitedAt: '3312-01-01T00:00:00Z',
        },
        number: 1,
        visitState: 'visited',
        visitedAtGalactic: '3312-01-01 00:00 UTC',
      }),
      index: 0,
      isLocked: false,
      lastVisitedIndex: 2,
    })

    // Origin #1 hides drag and delete controls
    expect(originHtml).toContain('title="Insert row below"')
    expect(originHtml).not.toContain('title="Hold to preview valid drop positions, drag to reorder waypoint"')
    expect(originHtml).not.toContain('title="Delete row"')
  })

  it('renders dashed drop-slot rows, collapsed visited summary rows, and unvisited gap line highlights during hold/drag', () => {
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
    expect(topGapRowHtml).toContain('*:shadow-[inset_0_2px_0_0_#00D2FF]')
    expect(topGapRowHtml).toContain('120.0 Ly')
    expect(topGapRowHtml).not.toContain('UNVISITED SLOT')

    const bottomGapRowHtml = renderRowItem({
      rowView: baseRow,
      bottomGapHighlight: true,
    })
    expect(bottomGapRowHtml).toContain('*:shadow-[inset_0_-2px_0_0_#00D2FF]')

    const grabbedRowHtml = renderRowItem({
      rowView: baseRow,
      isBeingDragged: true,
    })
    expect(grabbedRowHtml).toContain('data-being-dragged="true"')
    expect(grabbedRowHtml).toContain('MOVING')
    expect(grabbedRowHtml).toContain('*:shadow-[inset_0_2px_0_0_#FF7100,inset_0_-2px_0_0_#FF7100]')

    // Dashed valid historical drop-slot row
    const historicalSlotHtml = renderToString(
      createElement('table', null,
        createElement('tbody', null,
          createElement(PlannerDropSlotRow, {
            gapIndex: 2,
            visibleColumnCount: 8,
            grabbedSystemName: 'LP 355-65',
            preview: expedition.DropSlotPreview.createFrom({
              targetIndex: 2,
              status: 'valid_visited',
              visitedAt: '2026-01-18T20:10:00Z',
              visitedAtGalactic: '3312-01-18 20:10 UTC',
            }),
            isDragTarget: false,
            onDragOver: () => {},
            onDrop: () => {},
          }),
        ),
      ),
    )
    expect(historicalSlotHtml).toContain('border-dashed border-[#00D2FF]/80')
    expect(historicalSlotHtml).toContain('+ DROP LP 355-65 HERE')
    expect(historicalSlotHtml).toContain('LOG VISIT')
    expect(historicalSlotHtml).toContain('3312-01-18 20:10 UTC')

    // Dashed orange FC-blocked drop-slot row
    const fcBlockedSlotHtml = renderToString(
      createElement('table', null,
        createElement('tbody', null,
          createElement(PlannerDropSlotRow, {
            gapIndex: 1,
            visibleColumnCount: 8,
            grabbedSystemName: 'LP 355-65',
            preview: expedition.DropSlotPreview.createFrom({
              targetIndex: 1,
              status: 'blocked_fc',
            }),
            isDragTarget: false,
            onDragOver: () => {},
            onDrop: () => {},
          }),
        ),
      ),
    )
    expect(fcBlockedSlotHtml).toContain('border-dashed border-[#FF7100]')
    expect(fcBlockedSlotHtml).toContain('FC STOP ORDER BLOCKED')

    // Collapsed visited summary row
    const collapsedRowHtml = renderToString(
      createElement('table', null,
        createElement('tbody', null,
          createElement(PlannerCollapsedVisitedRow, {
            visibleColumnCount: 8,
            waypointCount: 6,
            regionCount: 0,
            firstWaypointNumber: 1,
            firstSystemName: 'HR 839',
            lastWaypointNumber: 6,
            lastSystemName: 'BD+41 4773',
          }),
        ),
      ),
    )
    expect(collapsedRowHtml).toContain('⋯ #01 HR 839 → #06 BD+41 4773')
    expect(collapsedRowHtml).toContain('(6 visited waypoints collapsed) ⋯')
  })

  it('enforces Fleet Carrier stop rules: + FC STOP after lastFcArrivedIndex, remove only on PENDING, and lock active route target', () => {
    const visitedAfterFcFrontierHtml = renderRowItem({
      rowView: expedition.RowView.createFrom({
        row: {
          id: 61,
          expeditionId: 1,
          rowType: 'waypoint',
          systemName: 'Gandharvi',
          visitedAt: '3312-01-12T00:00:00Z',
          fcStop: false,
        },
        number: 3,
        visitState: 'visited',
      }),
      index: 2,
      lastFcArrivedIndex: 1,
      lastVisitedIndex: 2,
    })
    expect(visitedAfterFcFrontierHtml).toContain('+ FC STOP')

    const visitedBeforeFcFrontierHtml = renderRowItem({
      rowView: expedition.RowView.createFrom({
        row: {
          id: 60,
          expeditionId: 1,
          rowType: 'waypoint',
          systemName: 'Rohini',
          visitedAt: '3312-01-10T00:00:00Z',
          fcStop: false,
        },
        number: 2,
        visitState: 'visited',
      }),
      index: 1,
      lastFcArrivedIndex: 2,
      lastVisitedIndex: 2,
    })
    expect(visitedBeforeFcFrontierHtml).not.toContain('+ FC STOP')

    const arrivedStopHtml = renderRowItem({
      rowView: expedition.RowView.createFrom({
        row: {
          id: 62,
          expeditionId: 1,
          rowType: 'waypoint',
          systemName: 'Colonia',
          fcStop: true,
          fcArrived: true,
        },
        number: 4,
        visitState: 'future',
      }),
      index: 3,
      lastFcArrivedIndex: 3,
      lastVisitedIndex: 2,
      onOverrideFcArrived: () => {},
    })
    expect(arrivedStopHtml).not.toContain('placeholder="Star System"')
    expect(arrivedStopHtml).toContain('title="Locked while Fleet Carrier is marked arrived at this waypoint"')
    expect(arrivedStopHtml).toContain('ARRIVED')
    expect(arrivedStopHtml).toContain('Carrier has arrived at this waypoint (click to override)')
    expect(arrivedStopHtml).not.toContain('Remove Fleet Carrier stop designation')

    const pendingStopHtml = renderRowItem({
      rowView: expedition.RowView.createFrom({
        row: {
          id: 63,
          expeditionId: 1,
          rowType: 'waypoint',
          systemName: 'Sagittarius A*',
          fcStop: true,
          fcArrived: false,
        },
        number: 5,
        visitState: 'future',
      }),
      index: 4,
      lastFcArrivedIndex: 3,
      lastVisitedIndex: 3,
    })
    expect(pendingStopHtml).toContain('PENDING')
    expect(pendingStopHtml).toContain('Remove Fleet Carrier stop designation')

    const activelyRoutedStopHtml = renderRowItem({
      rowView: expedition.RowView.createFrom({
        row: {
          id: 64,
          expeditionId: 1,
          rowType: 'waypoint',
          systemName: 'Beagle Point',
          fcStop: true,
          fcArrived: false,
        },
        number: 6,
        visitState: 'future',
      }),
      index: 5,
      isNextFcStop: true,
      hasActiveCarrierRoute: true,
      isActivelyRoutedFcStop: true,
      lastFcArrivedIndex: 3,
      lastVisitedIndex: 3,
    })
    expect(activelyRoutedStopHtml).toContain('ROUTING')
    expect(activelyRoutedStopHtml).toContain('title="Locked while Fleet Carrier route to this waypoint is active"')
  })
})

