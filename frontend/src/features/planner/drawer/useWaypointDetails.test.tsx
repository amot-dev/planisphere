// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { expedition } from '../../../api/models'
import { useWaypointDetails, type WaypointDetailsState } from './useWaypointDetails'

const getWaypointDetailsMock = vi.fn<(rowId: number) => Promise<expedition.WaypointDetails>>()

vi.mock('../../../api/backend', () => ({
  GetWaypointDetails: (rowId: number) => getWaypointDetailsMock(rowId),
}))

function createDeferred<T>() {
  let resolveValue!: (value: T) => void
  const promise = new Promise<T>((resolve) => {
    resolveValue = resolve
  })
  return { promise, resolve: resolveValue }
}

describe('useWaypointDetails', () => {
  afterEach(() => {
    getWaypointDetailsMock.mockReset()
  })

  it('shows loading on first expand, skips closed-drawer fetches across forward archive updates, and refreshes on departure or closedWindowRevision', async () => {
    ;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

    const activePlanInSystem = expedition.PlanView.createFrom({
      expedition: {
        id: 7,
        name: 'Selective Refresh Test',
        status: 'active',
        startTimestamp: '2026-01-01T10:00:00Z',
      },
      rows: [
        {
          row: {
            id: 101,
            expeditionId: 7,
            rowType: 'waypoint',
            systemName: 'Sol',
            visitedAt: '2026-01-01T10:00:00Z',
            departedAt: '2026-01-01T10:15:00Z',
          },
          number: 1,
          visitState: 'visited',
          drawerTo: '2026-01-01T10:15:00Z',
        },
        {
          row: {
            id: 102,
            expeditionId: 7,
            rowType: 'waypoint',
            systemName: 'Alpha Centauri',
            visitedAt: '2026-01-01T11:00:00Z',
            departedAt: '',
          },
          number: 2,
          visitState: 'visited',
          drawerFrom: '2026-01-01T10:15:00Z',
        },
      ],
    })

    const activePlanDeparted = expedition.PlanView.createFrom({
      ...activePlanInSystem,
      rows: activePlanInSystem.rows.map((rowView) =>
        rowView.row.id === 102
          ? {
              ...rowView,
              drawerTo: '2026-01-01T11:30:00Z',
              row: { ...rowView.row, departedAt: '2026-01-01T11:30:00Z' },
            }
          : rowView,
      ),
    })

    const firstLoad = createDeferred<expedition.WaypointDetails>()
    const backgroundRefresh = createDeferred<expedition.WaypointDetails>()
    const departureRefresh = createDeferred<expedition.WaypointDetails>()
    const retroactiveRefresh = createDeferred<expedition.WaypointDetails>()
    getWaypointDetailsMock
      .mockReturnValueOnce(firstLoad.promise)
      .mockReturnValueOnce(backgroundRefresh.promise)
      .mockReturnValueOnce(departureRefresh.promise)
      .mockReturnValueOnce(retroactiveRefresh.promise)

    let latestState!: WaypointDetailsState
    function Harness({
      plan,
      archiveRevision,
      closedWindowRevision = 1,
    }: {
      plan: expedition.PlanView
      archiveRevision: string
      closedWindowRevision?: number
    }) {
      latestState = useWaypointDetails(plan, false, archiveRevision, false, closedWindowRevision)
      return null
    }

    const container = document.createElement('div')
    const root = createRoot(container)
    await act(async () => {
      root.render(createElement(Harness, { plan: activePlanInSystem, archiveRevision: 'rev-1' }))
    })

    // First expand has no cached details yet, so loading must activate.
    await act(async () => {
      latestState.toggleExpand(102)
    })
    expect(latestState.expandedRowIds[102]).toBe(true)
    expect(latestState.loadingDetails[102]).toBe(true)

    const initialDetails = expedition.WaypointDetails.createFrom({
      rowId: 102,
      kind: 'leg',
      number: 2,
      from: '2026-01-01T10:15:00Z',
      to: '',
      arrivedAt: '2026-01-01T11:00:00Z',
      inSystem: true,
      shipsUsed: ['Mandalay'],
      metrics: { jumps: 4 },
    })
    await act(async () => {
      firstLoad.resolve(initialDetails)
      await firstLoad.promise
    })
    expect(latestState.loadingDetails[102]).toBe(false)
    expect(latestState.detailsById[102]?.metrics.jumps).toBe(4)

    // Subsequent archiveRevision refresh must keep existing details visible without toggling loading true.
    await act(async () => {
      root.render(createElement(Harness, { plan: activePlanInSystem, archiveRevision: 'rev-2' }))
    })
    expect(getWaypointDetailsMock).toHaveBeenCalledTimes(2)
    expect(latestState.loadingDetails[102]).toBe(false)
    expect(latestState.detailsById[102]?.metrics.jumps).toBe(4)

    const refreshedDetails = expedition.WaypointDetails.createFrom({
      rowId: 102,
      kind: 'leg',
      number: 2,
      from: '2026-01-01T10:15:00Z',
      to: '',
      arrivedAt: '2026-01-01T11:00:00Z',
      inSystem: true,
      shipsUsed: ['Mandalay'],
      metrics: { jumps: 4 },
      realization: { totalRealized: 5000000 },
    })
    await act(async () => {
      backgroundRefresh.resolve(refreshedDetails)
      await backgroundRefresh.promise
    })
    expect(latestState.loadingDetails[102]).toBe(false)
    expect(latestState.detailsById[102]?.realization.totalRealized).toBe(5000000)

    // Departure transition on the open drawer must trigger a flicker-free refresh even on the same archiveRevision.
    await act(async () => {
      root.render(createElement(Harness, { plan: activePlanDeparted, archiveRevision: 'rev-2' }))
    })
    expect(getWaypointDetailsMock).toHaveBeenCalledTimes(3)
    expect(latestState.loadingDetails[102]).toBe(false)

    const departedDetails = expedition.WaypointDetails.createFrom({
      rowId: 102,
      kind: 'leg',
      number: 2,
      from: '2026-01-01T10:15:00Z',
      to: '2026-01-01T11:30:00Z',
      arrivedAt: '2026-01-01T11:00:00Z',
      departedAt: '2026-01-01T11:30:00Z',
      shipsUsed: ['Mandalay'],
      metrics: { jumps: 4, bodiesScanned: 7 },
    })
    await act(async () => {
      departureRefresh.resolve(departedDetails)
      await departureRefresh.promise
    })
    expect(latestState.loadingDetails[102]).toBe(false)
    expect(latestState.detailsById[102]?.to).toBe('2026-01-01T11:30:00Z')
    expect(latestState.detailsById[102]?.metrics.bodiesScanned).toBe(7)

    // Closed drawers ignore forward archiveRevision updates while closedWindowRevision stays constant.
    await act(async () => {
      root.render(
        createElement(Harness, {
          plan: activePlanDeparted,
          archiveRevision: 'rev-3',
          closedWindowRevision: 1,
        }),
      )
    })
    expect(getWaypointDetailsMock).toHaveBeenCalledTimes(3)

    // Incrementing closedWindowRevision refreshes the closed drawer without loading flash.
    await act(async () => {
      root.render(
        createElement(Harness, {
          plan: activePlanDeparted,
          archiveRevision: 'rev-4',
          closedWindowRevision: 2,
        }),
      )
    })
    expect(getWaypointDetailsMock).toHaveBeenCalledTimes(4)
    expect(latestState.loadingDetails[102]).toBe(false)

    await act(async () => {
      retroactiveRefresh.resolve(
        expedition.WaypointDetails.createFrom({
          ...departedDetails,
          realization: { totalRealized: 19010800 },
        }),
      )
      await retroactiveRefresh.promise
    })
    expect(latestState.detailsById[102]?.realization.totalRealized).toBe(19010800)

    await act(async () => {
      root.unmount()
    })
  })

  it('prunes expanded state, cached details, and pending responses when a current row is demoted to future', async () => {
    ;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

    const originRow = {
      row: {
        id: 201,
        expeditionId: 9,
        rowType: 'waypoint',
        systemName: 'Sol',
        visitedAt: '2026-01-01T10:00:00Z',
        departedAt: '2026-01-01T10:15:00Z',
      },
      number: 1,
      visitState: 'visited',
      drawerTo: '2026-01-01T10:15:00Z',
    }

    const planWithCurrentRow = expedition.PlanView.createFrom({
      expedition: {
        id: 9,
        name: 'Future Prune Test',
        status: 'active',
        startTimestamp: '2026-01-01T10:00:00Z',
      },
      rows: [
        originRow,
        {
          row: { id: 203, expeditionId: 9, rowType: 'waypoint', systemName: 'Colonia' },
          number: 2,
          visitState: 'current',
          drawerFrom: '2026-01-01T10:15:00Z',
        },
      ],
    })

    const planAfterInsertDemotingToFuture = expedition.PlanView.createFrom({
      ...planWithCurrentRow,
      rows: [
        originRow,
        {
          row: { id: 202, expeditionId: 9, rowType: 'waypoint', systemName: 'Rohini' },
          number: 2,
          visitState: 'current',
          drawerFrom: '2026-01-01T10:15:00Z',
        },
        {
          row: { id: 203, expeditionId: 9, rowType: 'waypoint', systemName: 'Colonia' },
          number: 3,
          visitState: 'future',
        },
      ],
    })

    const initialCurrentLoad = createDeferred<expedition.WaypointDetails>()
    const inFlightBeforeDemote = createDeferred<expedition.WaypointDetails>()
    const emptyKindLoad = createDeferred<expedition.WaypointDetails>()
    getWaypointDetailsMock
      .mockReturnValueOnce(initialCurrentLoad.promise)
      .mockReturnValueOnce(inFlightBeforeDemote.promise)
      .mockReturnValueOnce(emptyKindLoad.promise)

    let latestState!: WaypointDetailsState
    function Harness({
      plan,
      archiveRevision,
    }: {
      plan: expedition.PlanView
      archiveRevision: string
    }) {
      latestState = useWaypointDetails(plan, false, archiveRevision, false, 1)
      return null
    }

    const container = document.createElement('div')
    const root = createRoot(container)
    await act(async () => {
      root.render(createElement(Harness, { plan: planWithCurrentRow, archiveRevision: 'rev-1' }))
    })

    await act(async () => {
      latestState.toggleExpand(203)
    })
    await act(async () => {
      initialCurrentLoad.resolve(
        expedition.WaypointDetails.createFrom({
          rowId: 203,
          kind: 'leg',
          number: 2,
          from: '2026-01-01T10:15:00Z',
          to: '',
          metrics: { jumps: 2 },
        }),
      )
      await initialCurrentLoad.promise
    })
    expect(latestState.expandedRowIds[203]).toBe(true)
    expect(latestState.detailsById[203]?.metrics.jumps).toBe(2)

    // Start an open-leg refresh, then demote row 203 to future before the response resolves.
    await act(async () => {
      root.render(createElement(Harness, { plan: planWithCurrentRow, archiveRevision: 'rev-2' }))
    })
    expect(getWaypointDetailsMock).toHaveBeenCalledTimes(2)

    await act(async () => {
      root.render(
        createElement(Harness, { plan: planAfterInsertDemotingToFuture, archiveRevision: 'rev-2' }),
      )
    })
    expect(latestState.expandedRowIds[203]).toBeUndefined()
    expect(latestState.detailsById[203]).toBeUndefined()
    expect(latestState.loadingDetails[203]).toBeUndefined()

    // Late resolution of the superseded in-flight request must be discarded.
    await act(async () => {
      inFlightBeforeDemote.resolve(
        expedition.WaypointDetails.createFrom({
          rowId: 203,
          kind: 'leg',
          number: 2,
          from: '2026-01-01T10:15:00Z',
          to: '',
          metrics: { jumps: 9 },
        }),
      )
      await inFlightBeforeDemote.promise
    })
    expect(latestState.detailsById[203]).toBeUndefined()

    // Promoting row 203 back to current must not auto-reopen or flash stale details.
    await act(async () => {
      root.render(createElement(Harness, { plan: planWithCurrentRow, archiveRevision: 'rev-2' }))
    })
    expect(latestState.expandedRowIds[203]).toBeUndefined()
    expect(latestState.detailsById[203]).toBeUndefined()
    expect(getWaypointDetailsMock).toHaveBeenCalledTimes(2)

    // Zero-valued payloads with empty kind must never be stored into detailsById.
    await act(async () => {
      latestState.toggleExpand(203)
    })
    await act(async () => {
      emptyKindLoad.resolve(expedition.WaypointDetails.createFrom({ rowId: 0, kind: '', number: 0 }))
      await emptyKindLoad.promise
    })
    expect(latestState.detailsById[203]).toBeUndefined()
    expect(latestState.loadingDetails[203]).toBe(false)

    await act(async () => {
      root.unmount()
    })
  })

  it('clears open drawers and discards stale in-flight responses across hot database switches with matching expedition IDs', async () => {
    ;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true
    const buildPlan = (secondSystem: string) =>
      expedition.PlanView.createFrom({
        expedition: { id: 1, name: 'Expedition One', status: 'active' },
        rows: [
          { row: { id: 101, expeditionId: 1, rowType: 'waypoint', systemName: 'Sol', visitedAt: '2026-01-01T10:00:00Z' }, number: 1, visitState: 'visited' },
          { row: { id: 102, expeditionId: 1, rowType: 'waypoint', systemName: secondSystem, visitedAt: '2026-01-01T11:00:00Z' }, number: 2, visitState: 'visited' },
        ],
      })

    const dbOneOriginLoad = createDeferred<expedition.WaypointDetails>()
    const dbOneInFlightLegLoad = createDeferred<expedition.WaypointDetails>()
    const dbTwoLegLoad = createDeferred<expedition.WaypointDetails>()
    getWaypointDetailsMock
      .mockReturnValueOnce(dbOneOriginLoad.promise)
      .mockReturnValueOnce(dbOneInFlightLegLoad.promise)
      .mockReturnValueOnce(dbTwoLegLoad.promise)

    let latestState!: WaypointDetailsState
    function Harness({ plan, databaseKey }: { plan: expedition.PlanView; databaseKey: string }) {
      latestState = useWaypointDetails(plan, false, 'rev-1', false, 1, databaseKey)
      return null
    }

    const root = createRoot(document.createElement('div'))
    await act(async () => { root.render(createElement(Harness, { plan: buildPlan('Colonia'), databaseKey: '/data/a.db' })) })
    await act(async () => { latestState.toggleExpand(101); latestState.toggleExpand(102) })
    await act(async () => {
      dbOneOriginLoad.resolve(expedition.WaypointDetails.createFrom({ rowId: 101, kind: 'origin', number: 1, metrics: { jumps: 5 } }))
      await dbOneOriginLoad.promise
    })
    expect(latestState.expandedRowIds[101]).toBe(true)
    expect(latestState.loadingDetails[102]).toBe(true)

    await act(async () => { root.render(createElement(Harness, { plan: buildPlan('Beagle Point'), databaseKey: '/data/b.db' })) })
    expect(latestState.expandedRowIds).toEqual({})
    expect(latestState.detailsById).toEqual({})
    expect(latestState.loadingDetails).toEqual({})

    await act(async () => { latestState.toggleExpand(102) })
    await act(async () => {
      dbOneInFlightLegLoad.resolve(expedition.WaypointDetails.createFrom({ rowId: 102, kind: 'leg', number: 2, metrics: { jumps: 99 } }))
      await dbOneInFlightLegLoad.promise
    })
    expect(latestState.detailsById[102]).toBeUndefined()
    expect(latestState.loadingDetails[102]).toBe(true)

    await act(async () => {
      dbTwoLegLoad.resolve(expedition.WaypointDetails.createFrom({ rowId: 102, kind: 'leg', number: 2, metrics: { jumps: 12 } }))
      await dbTwoLegLoad.promise
    })
    expect(latestState.detailsById[102]?.metrics.jumps).toBe(12)
    expect(latestState.loadingDetails[102]).toBe(false)
    await act(async () => { root.unmount() })
  })

  it('refreshes open closed-leg drawers in place without loading flash when expedition commander scope changes', async () => {
    ;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true
    const buildScopedPlan = (commanderScope: string, commanderProfileIds: number[]) =>
      expedition.PlanView.createFrom({
        expedition: {
          id: 12,
          name: 'Scope Change Refresh',
          status: 'completed',
          commanderScope,
          commanderProfileIds,
        },
        rows: [
          {
            row: {
              id: 301,
              expeditionId: 12,
              rowType: 'waypoint',
              systemName: 'Sol',
              visitedAt: '2026-01-01T10:00:00Z',
              departedAt: '2026-01-01T10:15:00Z',
            },
            number: 1,
            visitState: 'visited',
            drawerTo: '2026-01-01T10:15:00Z',
          },
        ],
      })

    const initialOriginLoad = createDeferred<expedition.WaypointDetails>()
    const scopedOriginRefresh = createDeferred<expedition.WaypointDetails>()
    getWaypointDetailsMock
      .mockReturnValueOnce(initialOriginLoad.promise)
      .mockReturnValueOnce(scopedOriginRefresh.promise)

    let latestState!: WaypointDetailsState
    function Harness({ plan }: { plan: expedition.PlanView }) {
      latestState = useWaypointDetails(plan, false, 'rev-1', false, 1, '/data/main.db')
      return null
    }

    const root = createRoot(document.createElement('div'))
    await act(async () => {
      root.render(createElement(Harness, { plan: buildScopedPlan('all_enabled', []) }))
    })
    await act(async () => {
      latestState.toggleExpand(301)
    })
    await act(async () => {
      initialOriginLoad.resolve(
        expedition.WaypointDetails.createFrom({
          rowId: 301,
          kind: 'origin',
          number: 1,
          from: '2024-01-01T00:00:00Z',
          to: '2026-01-01T10:15:00Z',
          arrivedAt: '2026-01-01T10:00:00Z',
          metrics: { jumps: 50 },
        }),
      )
      await initialOriginLoad.promise
    })
    expect(latestState.detailsById[301]?.metrics.jumps).toBe(50)

    await act(async () => {
      root.render(createElement(Harness, { plan: buildScopedPlan('selected', [2]) }))
    })
    expect(getWaypointDetailsMock).toHaveBeenCalledTimes(2)
    expect(latestState.loadingDetails[301]).toBe(false)

    await act(async () => {
      scopedOriginRefresh.resolve(
        expedition.WaypointDetails.createFrom({
          rowId: 301,
          kind: 'origin',
          number: 1,
          from: '2025-06-01T00:00:00Z',
          to: '2026-01-01T10:15:00Z',
          arrivedAt: '2026-01-01T10:00:00Z',
          metrics: { jumps: 14 },
        }),
      )
      await scopedOriginRefresh.promise
    })
    expect(latestState.detailsById[301]?.metrics.jumps).toBe(14)
    await act(async () => {
      root.unmount()
    })
  })
})

