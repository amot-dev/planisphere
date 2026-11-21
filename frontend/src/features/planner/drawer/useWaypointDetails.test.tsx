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

  it('shows loading on first expand but preserves loaded telemetry without loading flash on background refresh', async () => {
    ;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

    const activePlan = expedition.PlanView.createFrom({
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
          },
          number: 1,
          visitState: 'visited',
        },
        {
          row: {
            id: 102,
            expeditionId: 7,
            rowType: 'waypoint',
            systemName: 'Alpha Centauri',
            visitedAt: '2026-01-01T11:00:00Z',
          },
          number: 2,
          visitState: 'visited',
        },
      ],
    })

    const firstLoad = createDeferred<expedition.WaypointDetails>()
    const backgroundRefresh = createDeferred<expedition.WaypointDetails>()
    getWaypointDetailsMock
      .mockReturnValueOnce(firstLoad.promise)
      .mockReturnValueOnce(backgroundRefresh.promise)

    let latestState!: WaypointDetailsState
    function Harness({ archiveRevision }: { archiveRevision: string }) {
      latestState = useWaypointDetails(activePlan, false, archiveRevision)
      return null
    }

    const container = document.createElement('div')
    const root = createRoot(container)
    await act(async () => {
      root.render(createElement(Harness, { archiveRevision: 'rev-1' }))
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
      from: '2026-01-01T10:00:00Z',
      to: '2026-01-01T11:00:00Z',
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
      root.render(createElement(Harness, { archiveRevision: 'rev-2' }))
    })
    expect(getWaypointDetailsMock).toHaveBeenCalledTimes(2)
    expect(latestState.loadingDetails[102]).toBe(false)
    expect(latestState.detailsById[102]?.metrics.jumps).toBe(4)

    const refreshedDetails = expedition.WaypointDetails.createFrom({
      rowId: 102,
      kind: 'leg',
      number: 2,
      from: '2026-01-01T10:00:00Z',
      to: '2026-01-01T11:00:00Z',
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

    await act(async () => {
      root.unmount()
    })
  })
})

