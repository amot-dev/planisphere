import { createElement } from 'react'
import { renderToString } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { expedition } from '../../../api/models'
import { PlannerRouteTable } from './PlannerRouteTable'

describe('PlannerRouteTable Carrier Locks and Region Borders', () => {
  it('enforces manual vs Spansh carrier route locking behavior on routed waypoints', () => {
    const routePlan = expedition.PlanView.createFrom({
      expedition: { id: 1, status: 'active' },
      rows: [
        {
          row: {
            id: 10,
            rowType: 'waypoint',
            systemName: 'Sol',
            visitedAt: '3312-01-01T00:00:00Z',
          },
          number: 1,
          visitState: 'visited',
        },
        {
          row: {
            id: 11,
            rowType: 'waypoint',
            systemName: 'Colonia',
            fcStop: true,
            fcArrived: false,
          },
          number: 2,
          visitState: 'future',
        },
      ],
    })
    const tableItems = routePlan.rows.map((rowView, index) => ({
      kind: 'waypoint' as const,
      rowView,
      index,
      key: `row-${rowView.row.id}`,
    }))
    const baseCarrierState = {
      activeCarrierRoute: expedition.CarrierRouteResult.createFrom({
        origin: 'Sol',
        destination: 'Colonia',
        jumps: [],
      }),
      activeCarrierTargetRowId: 11,
      nextFcStopEntry: { row: routePlan.rows[1].row, index: 1 },
      routedCarrierStopIndex: 1,
      onOpenCarrierRoutePrompt: () => {},
    }
    const baseTableProps = {
      plan: routePlan,
      isPlanning: false,
      isCompleted: false,
      frontier: { lastVisitedIndex: 0, lastFcArrivedIndex: -1 },
      columns: {
        visibleColumnCount: 8,
        tableMinWidthPixels: 1200,
        showNotes: true,
        showCarrierStop: true,
        onToggleShowNotes: () => {},
        onToggleShowCarrierStop: () => {},
      },
      externalLinks: {},
      drawerState: {
        expandedRowIds: {},
        detailsById: {},
        loadingDetails: {},
        toggleExpand: () => {},
      },
      dragState: {
        tableItems,
        dropSlotGaps: new Set<number>(),
        heldOrDraggedIndex: null,
        draggedIndex: null,
        dragOverGapIndex: null,
        getRowDropProps: () => ({}),
        getSlotDropProps: () => ({ onDragOver: () => {}, onDrop: () => {} }),
        onClearDragOverGap: () => {},
        onRowHoldStart: () => {},
        onRowHoldEnd: () => {},
        onRowDragStart: () => {},
        onRowDragEnd: () => {},
      },
      rowActions: {
        copiedSystemRowId: null,
        insertMenuAfterId: null,
        onSetInsertMenuAfterId: () => {},
        onChangeRegion: () => {},
        onInsert: () => {},
        onDeleteRow: () => {},
        onLegModeChange: () => {},
        onFieldBlur: () => {},
        onCopySystem: () => {},
        onToggleFcStop: () => {},
        onOverrideFcArrived: () => {},
      },
    }
    const manualTableHtml = renderToString(
      createElement(PlannerRouteTable, {
        ...baseTableProps,
        carrierState: { ...baseCarrierState, isManualCarrierRoute: true },
      }),
    )
    expect(manualTableHtml).toContain('placeholder="Star System"')
    expect(manualTableHtml).not.toContain('Locked while Fleet Carrier route to this waypoint is active')

    const spanshTableHtml = renderToString(
      createElement(PlannerRouteTable, {
        ...baseTableProps,
        carrierState: { ...baseCarrierState, isManualCarrierRoute: false },
      }),
    )
    expect(spanshTableHtml).toContain('Locked while Fleet Carrier route to this waypoint is active')
  })

  it('adds a left border to region headers if and only if the following row has a border, matching its color', () => {
    const regionBorderPlan = expedition.PlanView.createFrom({
      expedition: {
        id: 21,
        name: 'Region Border Test',
        description: '',
        status: 'active',
        createdAt: '3310-01-01T00:00:00Z',
        startTimestamp: '3310-01-01T00:00:00Z',
      },
      rows: [
        {
          row: { id: 901, expeditionId: 21, position: 1000, rowType: 'region', regionId: 18 },
          number: 0,
          regionName: 'Inner Orion Spur',
          visitState: 'future',
        },
        {
          row: { id: 902, expeditionId: 21, position: 2000, rowType: 'waypoint', systemName: 'Sol' },
          number: 1,
          visitState: 'visited',
          visitedAtGalactic: '3310-01-01 00:00',
        },
        {
          row: { id: 903, expeditionId: 21, position: 3000, rowType: 'region', regionId: 17 },
          number: 0,
          regionName: 'Norma Expanse',
          visitState: 'future',
        },
        {
          row: { id: 904, expeditionId: 21, position: 4000, rowType: 'region', regionId: 16 },
          number: 0,
          regionName: 'Norma Arm',
          visitState: 'future',
        },
        {
          row: { id: 905, expeditionId: 21, position: 5000, rowType: 'waypoint', systemName: 'Rohini' },
          number: 2,
          visitState: 'visited',
          visitedAtGalactic: '3310-01-02 00:00',
          legModeId: 1,
          legModeColor: '#00D2FF',
        },
        {
          row: { id: 906, expeditionId: 21, position: 6000, rowType: 'region', regionId: 1 },
          number: 0,
          regionName: 'Galactic Centre',
          visitState: 'future',
        },
        {
          row: {
            id: 907,
            expeditionId: 21,
            position: 7000,
            rowType: 'waypoint',
            systemName: 'Sagittarius A*',
          },
          number: 3,
          visitState: 'current',
          legModeId: 2,
          legModeColor: '#00E5A3',
        },
        {
          row: { id: 908, expeditionId: 21, position: 8000, rowType: 'region', regionId: 2 },
          number: 0,
          regionName: 'Empyrean Straits',
          visitState: 'future',
        },
        {
          row: {
            id: 909,
            expeditionId: 21,
            position: 9000,
            rowType: 'waypoint',
            systemName: 'Beagle Point',
          },
          number: 4,
          visitState: 'future',
        },
        {
          row: { id: 910, expeditionId: 21, position: 10000, rowType: 'region', regionId: 3 },
          number: 0,
          regionName: "Ryker's Hope",
          visitState: 'future',
        },
      ],
      waypointCount: 4,
      visitedWaypointCount: 2,
    })

    const tableItems = regionBorderPlan.rows.map((rowView, index) => ({
      kind: (rowView.row.rowType === 'region' ? 'region' : 'waypoint') as 'region' | 'waypoint',
      rowView,
      index,
      key: `${rowView.row.rowType}-${rowView.row.id}`,
    }))

    const html = renderToString(
      createElement(PlannerRouteTable, {
        plan: regionBorderPlan,
        isPlanning: false,
        isCompleted: false,
        frontier: { lastVisitedIndex: 4, lastFcArrivedIndex: -1 },
        columns: {
          visibleColumnCount: 8,
          tableMinWidthPixels: 1200,
          showNotes: true,
          showCarrierStop: true,
          onToggleShowNotes: () => {},
          onToggleShowCarrierStop: () => {},
        },
        externalLinks: {},
        drawerState: {
          expandedRowIds: {},
          detailsById: {},
          loadingDetails: {},
          toggleExpand: () => {},
        },
        dragState: {
          tableItems,
          dropSlotGaps: new Set<number>(),
          heldOrDraggedIndex: null,
          draggedIndex: null,
          dragOverGapIndex: null,
          getRowDropProps: () => ({}),
          getSlotDropProps: () => ({ onDragOver: () => {}, onDrop: () => {} }),
          onClearDragOverGap: () => {},
          onRowHoldStart: () => {},
          onRowHoldEnd: () => {},
          onRowDragStart: () => {},
          onRowDragEnd: () => {},
        },
        carrierState: {
          activeCarrierRoute: null,
          activeCarrierTargetRowId: 0,
          isManualCarrierRoute: false,
          nextFcStopEntry: null,
          routedCarrierStopIndex: -1,
          onOpenCarrierRoutePrompt: () => {},
        },
        rowActions: {
          copiedSystemRowId: null,
          insertMenuAfterId: null,
          onSetInsertMenuAfterId: () => {},
          onChangeRegion: () => {},
          onInsert: () => {},
          onDeleteRow: () => {},
          onLegModeChange: () => {},
          onFieldBlur: () => {},
          onCopySystem: () => {},
          onToggleFcStop: () => {},
          onOverrideFcArrived: () => {},
        },
      }),
    )

    const rowHtml = (rowId: number) => {
      const match = html.match(new RegExp(`<tr[^>]*data-row-id="${rowId}"[\\s\\S]*?<\\/tr>`))
      return match ? match[0] : ''
    }

    // Region before Origin (#01) matches Origin muted neutral border
    expect(rowHtml(901)).toContain('style="border-left-color:#3d2816"')
    expect(rowHtml(901)).toContain('border-l-2 shadow-[inset_0_-1px_0_0_#3a2615]')

    // Consecutive regions before visited waypoint (#02) both inherit its flight mode border
    expect(rowHtml(903)).toContain('style="border-left-color:#00D2FF"')
    expect(rowHtml(904)).toContain('style="border-left-color:#00D2FF"')

    // Region before in-progress current waypoint (#03) inherits its active flight mode border
    expect(rowHtml(906)).toContain('style="border-left-color:#00E5A3"')

    // Region before future waypoint (#04) and trailing region have no left border
    expect(rowHtml(908)).not.toContain('border-l-2')
    expect(rowHtml(910)).not.toContain('border-l-2')
  })
})

