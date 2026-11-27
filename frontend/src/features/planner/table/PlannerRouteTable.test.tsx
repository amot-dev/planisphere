import { renderToString } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { expedition } from '../../../api/models'
import { PlannerRouteTable } from './PlannerRouteTable'

describe('PlannerRouteTable Carrier Locks and Region Borders', () => {
  const renderTable = (plan: expedition.PlanView, insertMenuAfterId: number | null = null) => {
    const tableItems = plan.rows.map((rowView, index) => ({
      kind: (rowView.row.rowType === 'region' ? 'region' : 'waypoint') as 'region' | 'waypoint',
      rowView,
      index,
      key: `${rowView.row.rowType}-${rowView.row.id}`,
    }))
    return renderToString(
      <PlannerRouteTable
        plan={plan}
        isPlanning={false}
        isCompleted={false}
        columns={{
          visibleColumnCount: 8,
          tableMinWidthPixels: 1200,
          showNotes: true,
          showCarrierStop: true,
          onToggleShowNotes: () => {},
          onToggleShowCarrierStop: () => {},
        }}
        externalLinks={{}}
        drawerState={{
          expandedRowIds: {},
          detailsById: {},
          loadingDetails: {},
          toggleExpand: () => {},
        }}
        dragState={{
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
        }}
        rowActions={{
          copiedSystemRowId: null,
          insertMenuAfterId,
          onSetInsertMenuAfterId: () => {},
          onChangeRegion: () => {},
          onInsert: () => {},
          onDeleteRow: () => {},
          onLegModeChange: () => {},
          onFieldBlur: () => {},
          onCopySystem: () => {},
          onToggleFcStop: () => {},
          onOverrideFcArrived: () => {},
          onOpenCarrierRoutePrompt: () => {},
        }}
      />,
    )
  }

  it('renders manual vs Spansh carrier route locks, drawer cascade flag, and region insert flag from Go permissions', () => {
    const buildPlanWithTargetPermissions = (
      targetPermissions: Partial<expedition.WaypointPermissions>,
    ) =>
      expedition.PlanView.createFrom({
        expedition: { id: 1, status: 'active' },
        lastVisitedIndex: 1,
        lastFcArrivedIndex: -1,
        rows: [
          {
            row: { id: 9, rowType: 'region', regionId: 18 },
            number: 0,
            regionName: 'Inner Orion Spur',
            allowInsertWaypoint: false,
          },
          {
            row: {
              id: 10,
              rowType: 'waypoint',
              systemName: 'Sol',
              visitedAt: '3312-01-01T00:00:00Z',
            },
            number: 1,
            visitState: 'visited',
            allowInsertWaypoint: true,
            permissions: {
              systemLocked: true,
              allowInsertWaypoint: true,
              canReorder: false,
              deleteDisabledReason: 'Origin waypoint cannot be deleted',
              reorderDisabledReason: 'Origin waypoint cannot be reordered',
              canAddFcStop: false,
              canRemoveFcStop: false,
              canOverrideFcBadge: false,
              canCascadeLegMode: false,
              hasDrawer: true,
              isFcStopLocked: true,
            },
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
            allowInsertWaypoint: true,
            permissions: {
              systemLocked: false,
              allowInsertWaypoint: true,
              canReorder: true,
              canAddFcStop: true,
              canRemoveFcStop: true,
              canOverrideFcBadge: true,
              hasDrawer: false,
              hasActiveCarrierRoute: true,
              ...targetPermissions,
            },
          },
        ],
      })

    const manualTableHtml = renderTable(buildPlanWithTargetPermissions({ isFcStopLocked: false }), 9)
    expect(manualTableHtml).toContain('placeholder="Star System"')
    expect(manualTableHtml).toContain('ROUTING')
    expect(manualTableHtml).toContain(
      'Add new waypoints in the unvisited section and drag them into valid past slots.',
    )
    expect(manualTableHtml).not.toContain('Locked while Fleet Carrier route to this waypoint is active')

    const spanshTableHtml = renderTable(
      buildPlanWithTargetPermissions({
        systemLocked: true,
        systemLockReason: 'Locked while Fleet Carrier route to this waypoint is active',
        canReorder: false,
        deleteDisabledReason: 'Locked while Fleet Carrier route to this waypoint is active',
        reorderDisabledReason: 'Locked while Fleet Carrier route to this waypoint is active',
        canRemoveFcStop: false,
        canOverrideFcBadge: false,
        isFcStopLocked: true,
        isSpanshRoutedFcStop: true,
      }),
      9,
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
      lastVisitedIndex: 4,
      lastFcArrivedIndex: -1,
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

    const html = renderTable(regionBorderPlan)

    const rowHtml = (rowId: number) => {
      const match = html.match(new RegExp(`<tr[^>]*data-row-id="${rowId}"[\\s\\S]*?<\\/tr>`))
      return match ? match[0] : ''
    }

    // Region before Origin (#01) matches Origin muted neutral border
    expect(rowHtml(901)).toContain('style="border-left-color:#3d2816"')
    expect(rowHtml(901)).toContain('border-l-2 shadow-[inset_0_-1px_0_0_var(--color-ed-border)]')

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
