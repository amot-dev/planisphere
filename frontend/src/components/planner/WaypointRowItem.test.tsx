import { describe, expect, it } from 'vitest'
import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { WaypointRowItem } from './WaypointRowItem'
import { AutoExpandingTextarea } from '../common/AutoExpandingTextarea'
import { WaypointDetailsDrawer } from './WaypointDetailsDrawer'
import { expedition } from '../../../wailsjs/go/models'

describe('WaypointRowItem and AutoExpandingTextarea Row Height Consistency', () => {
  function renderRowItem(props: Partial<Parameters<typeof WaypointRowItem>[0]> & { rowView: expedition.RowView }) {
    const defaultProps: Parameters<typeof WaypointRowItem>[0] = {
      index: 1,
      isPlanning: false,
      isLocked: false,
      isBeingDragged: false,
      isDragTarget: false,
      isExpanded: false,
      isNextFcStop: false,
      hasActiveCarrierRoute: false,
      draft: {
        systemName: props.rowView.row?.systemName ?? '',
        poiName: props.rowView.row?.poiName ?? '',
        notes: props.rowView.row?.notes ?? '',
      },
      copiedSystemRowId: null,
      insertMenuOpen: false,
      lastVisitedIndex: 0,
      totalRows: 5,
      rowStyle: {},
      dropProps: {},
      onToggleExpand: () => {},
      onDraftChange: () => {},
      onFieldFocus: () => {},
      onFieldBlur: () => {},
      onCopySystem: () => {},
      onToggleFcStop: () => {},
      onOpenCarrierRoutePrompt: () => {},
      onToggleInsertMenu: () => {},
      onInsert: () => {},
      onMove: () => {},
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

  it('AutoExpandingTextarea renders with rows="1" and min-h-[28px] without artificial wrap for 43-char LFT 509 notes', () => {
    const lftNotes = 'Returning to Bubble to participate in event'
    const html = renderToString(
      createElement(AutoExpandingTextarea, {
        value: lftNotes,
        placeholder: 'Flight notes, reminders...',
        onChange: () => {},
        onBlur: () => {},
      }),
    )

    // Ensure native rows is 1 and not artificially forced to 2
    expect(html).toContain('rows="1"')
    expect(html).not.toContain('rows="2"')
    expect(html).toContain('min-h-[28px]')
    expect(html).toContain('py-[3px]')
  })

  it('renders visited row with uniform h-7 (28px) elements and align-top cells', () => {
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
      totalRows: 25,
    })

    // Cell padding py-1.5 and align-top on all cells
    expect(html).toContain('align-top')
    expect(html).toContain('py-1.5')

    // Uniform 28px (h-7) on chevron, waypoint #, star system, carrier stop, telemetry, and controls
    expect(html).toContain('h-7 flex items-center justify-center') // Chevron container
    expect(html).toContain('h-7 flex items-center justify-end font-mono-tabular font-bold text-[#FF7100] text-xs leading-5') // Waypoint #
    expect(html).toContain('w-full h-7 flex items-center font-mono-tabular font-bold text-[#F4ECE4] px-1.5 py-[3px] border border-transparent select-text cursor-text truncate text-xs leading-5') // Star system locked
    expect(html).toContain('h-7 inline-flex items-center justify-center gap-1.5 whitespace-nowrap') // Carrier stop container
    expect(html).toContain('h-7 flex items-center gap-1.5 whitespace-nowrap font-mono-tabular text-[10px] leading-5') // Telemetry container (visited)
    expect(html).toContain('h-7 inline-flex items-center justify-end gap-1 whitespace-nowrap') // Controls container

    // Textareas have min-h-[28px] and rows=1
    expect(html).toContain('min-h-[28px]')
    expect(html).toContain('rows="1"')
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
      visitState: 'unvisited',
      distFromPrevLy: 42.5,
    })

    const html = renderRowItem({
      rowView: mockRowView,
      index: 16,
      lastVisitedIndex: 10,
      totalRows: 25,
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
      totalRows: 25,
    })

    expect(html).toContain('ARRIVED')
    expect(html).toContain('h-5 px-1.5 inline-flex items-center justify-center')
    expect(html).toContain('h-7 inline-flex items-center justify-center gap-1.5 whitespace-nowrap')
  })

  it('renders visited chevron cell with inset shadow to prevent bottom border from clipping the 2px orange left border', () => {
    const mockRowView = expedition.RowView.createFrom({
      row: {
        id: 12,
        expeditionId: 1,
        rowType: 'waypoint',
        systemName: 'Sol',
        seq: 11,
      },
      number: 12,
      visitState: 'visited',
    })

    const html = renderRowItem({
      rowView: mockRowView,
      index: 11,
      isLocked: true,
      isExpanded: true,
      lastVisitedIndex: 11,
      totalRows: 25,
    })

    // Chevron cell has border-l-2 and inset shadow bottom line so it never miters or clips the left border
    expect(html).toContain('border-l-2 border-l-[#FF7100] shadow-[inset_0_-1px_0_0_#24180e]')
  })

  it('renders WaypointDetailsDrawer with matching border-l-2 and inset shadow top line without clipping', () => {
    const mockRowView = expedition.RowView.createFrom({
      row: {
        id: 12,
        expeditionId: 1,
        rowType: 'waypoint',
        systemName: 'Sol',
      },
      number: 12,
      visitState: 'visited',
    })

    const mockDetails = expedition.WaypointDetails.createFrom({
      kind: 'historical',
      number: 12,
      metrics: {
        distanceLy: 350.67,
        shipDistanceLy: 350.67,
        jumps: 15,
        jetConeBoosts: 0,
        premiumBoosts: 0,
        standardBoosts: 0,
        basicBoosts: 0,
        bodiesScanned: 24,
        bodiesMapped: 6,
        firstDiscoveries: 0,
        firstMapped: 0,
        scanValueTotal: 1250000,
        scanValueBase: 1000000,
        scanValueBonus: 250000,
      },
    })

    const html = renderToString(
      createElement(WaypointDetailsDrawer, {
        details: mockDetails,
        loading: false,
        rowView: mockRowView,
      }),
    )

    // Matches header 2px border and uses inset shadow top line
    expect(html).toContain('border-l-2 border-l-[#FF7100] shadow-[inset_0_1px_0_0_#3d2816]')
    expect(html).not.toContain('border-l-3')
  })

  it('renders WaypointDetailsDrawer with Vessel Flown and 1-Click Mode toggle for leg waypoints (number > 1)', () => {
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

    expect(html).toContain('[VESSEL FLOWN]:')
    expect(html).toContain('Anaconda')
    expect(html).toContain('[MODE]:')
    expect(html).toContain('Exploration')
    expect(html).toContain('Active mode for this leg (click to change)')
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

    expect(html).toContain('WAYPOINT #1 // HISTORICAL BASELINE')
    expect(html).not.toContain('[VESSEL FLOWN]:')
    expect(html).not.toContain('[MODE]:')
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
    expect(html).toContain('Cartographics Lost (Death):')
    expect(html).toContain('-5,000,000 Cr')

    expect(html).toContain('Exobiology Lost (Death):')
    expect(html).toContain('-10,000,000 Cr')

    expect(html).toContain('Cartographics Stolen:')
    expect(html).toContain('-2,000,000 Cr')

    expect(html).toContain('Exobiology Stolen:')
    expect(html).toContain('-8,000,000 Cr')

    expect(html).toContain('Carrier Tariffs:')
    expect(html).toContain('-3,500,000 Cr')

    expect(html).toContain('NPC Crew Payouts:')
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
      visitState: 'unvisited',
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
      visitState: 'future',
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

    // Should display distance from player (42.1 Ly) with player location tooltip, not 380.5 Ly
    expect(html).toContain('42.1 Ly')
    expect(html).not.toContain('380.5 Ly')
    expect(html).toContain('Estimated straight-line distance from player location (Merope)')
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
})

