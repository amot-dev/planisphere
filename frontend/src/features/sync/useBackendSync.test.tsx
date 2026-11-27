// @vitest-environment happy-dom
import { act } from 'react'
import { describe, expect, it } from 'vitest'
import { app, expedition, stats } from '../../api/models'
import {
  makeMode,
  makePlanView,
  makeSettings,
  makeSidebarStats,
} from '../../test/fixtures'
import { renderHook } from '../../test/render'
import { installWailsAppMock, installWailsRuntimeMock } from '../../test/wailsMock'
import { isExpeditionExportEnabled, useBackendSync } from './useBackendSync'

describe('isExpeditionExportEnabled', () => {
  it('requires activeExpeditionId equality on both plan and sidebarStats branches', () => {
    const completedPlan = expedition.PlanView.createFrom({
      expedition: { id: 1, name: 'Completed Exp', status: 'completed' },
      rows: [],
      modes: [],
    })
    const planningPlan = expedition.PlanView.createFrom({
      expedition: { id: 2, name: 'Planning Exp', status: 'planning' },
      rows: [],
      modes: [],
    })
    const activePlan = expedition.PlanView.createFrom({
      expedition: { id: 2, name: 'Active Exp', status: 'active' },
      rows: [],
      modes: [],
    })
    const departedStats = app.SidebarStats.createFrom({ expeditionId: 1, departed: true })
    const undepartedStats = app.SidebarStats.createFrom({ expeditionId: 2, departed: false })
    const activeDepartedStats = app.SidebarStats.createFrom({ expeditionId: 2, departed: true })

    // Stale completed plan or stale departed stats from expedition 1 must not enable export on expedition 2.
    expect(isExpeditionExportEnabled(2, completedPlan, undepartedStats)).toBe(false)
    expect(isExpeditionExportEnabled(2, planningPlan, departedStats)).toBe(false)
    expect(isExpeditionExportEnabled(0, completedPlan, departedStats)).toBe(false)

    // Matching activeExpeditionId enables export when departed or completed.
    expect(isExpeditionExportEnabled(1, completedPlan, undepartedStats)).toBe(true)
    expect(isExpeditionExportEnabled(2, activePlan, activeDepartedStats)).toBe(true)
  })
})

describe('useBackendSync lifecycle', () => {
  it('hydrates bootstrap RPCs, flushes deferred mid-boot data:changed events on boot completion, and switches expeditions via SetActiveExpedition', async () => {
    const runtimeMock = installWailsRuntimeMock()

    let resolveBootPlan!: (plan: expedition.PlanView) => void
    const travelMode = makeMode({ id: 1, name: 'Travel', color: '#FF7100' })
    const surveyMode = makeMode({ id: 2, name: 'Survey', color: '#00CCFF' })

    const { spies } = installWailsAppMock({
      SetLiveSyncPaused: async () => {},
      GetSettings: async () =>
        makeSettings({
          setupCompleted: true,
          activeExpeditionId: 3,
          dbPath: '/tmp/planisphere.db',
        }),
      GetIngestStatus: async () =>
        app.IngestStatus.createFrom({
          filesTracked: 5,
          eventsStored: 240,
          scanning: false,
        }),
      GetModesState: async () => app.ModesState.createFrom({ modes: [travelMode, surveyMode] }),
      ListExpeditions: async () => [
        expedition.Expedition.createFrom({ id: 3, name: 'Sagittarius Loop', status: 'active' }),
        expedition.Expedition.createFrom({ id: 8, name: 'Formidine Spur', status: 'planning' }),
      ],
      GetPlan: (expeditionId: number) => {
        if (expeditionId === 3 && resolveBootPlan === undefined) {
          return new Promise<expedition.PlanView>((resolve) => {
            resolveBootPlan = resolve
          })
        }
        return Promise.resolve(
          makePlanView({
            expedition: {
              id: expeditionId,
              name: expeditionId === 3 ? 'Sagittarius Loop' : 'Formidine Spur',
              status: expeditionId === 3 ? 'active' : 'planning',
            },
            modes: [travelMode, surveyMode],
          }),
        )
      },
      GetSidebarStats: async (expeditionId: number, _requestedFilter: stats.Filter) =>
        makeSidebarStats({ expeditionId, status: expeditionId === 3 ? 'active' : 'planning' }),
      SetActiveExpedition: async () => {},
    })

    const mountedHook = renderHook(() => useBackendSync())
    expect(mountedHook.getResult().booting).toBe(true)
    expect(mountedHook.getResult().activeStatusMessage).toBe('INITIALIZING TERMINAL')

    // Let core bootstrap RPCs resolve while GetPlan(3) remains in flight.
    await act(async () => {
      await Promise.resolve()
    })
    expect(mountedHook.getResult().booting).toBe(true)
    expect(spies.GetSettings).toHaveBeenCalledTimes(1)
    expect(spies.GetIngestStatus).toHaveBeenCalledTimes(1)
    expect(spies.GetModesState).toHaveBeenCalledTimes(1)
    expect(spies.ListExpeditions).toHaveBeenCalledTimes(1)
    expect(spies.GetPlan).toHaveBeenCalledWith(3)
    expect(spies.GetSidebarStats).toHaveBeenCalledWith(3, expect.anything())

    // Emit mid-boot data:changed events after core state loaded; they must be deferred until boot completes.
    act(() => {
      runtimeMock.emitDataChanged({ domains: ['ingest', 'settings'] })
      runtimeMock.emitDataChanged({ domains: ['ingest'] })
    })
    expect(spies.GetIngestStatus).toHaveBeenCalledTimes(1)
    expect(spies.GetSettings).toHaveBeenCalledTimes(1)

    // Complete initial GetPlan(3): booting turns false and deferred ingest + settings reloads flush once each.
    await act(async () => {
      resolveBootPlan(
        makePlanView({
          expedition: { id: 3, name: 'Sagittarius Loop', status: 'active' },
          modes: [travelMode, surveyMode],
        }),
      )
      await Promise.resolve()
    })
    expect(mountedHook.getResult().booting).toBe(false)
    expect(mountedHook.getResult().activeExpeditionId).toBe(3)
    expect(mountedHook.getResult().plan?.expedition.name).toBe('Sagittarius Loop')
    expect(mountedHook.getResult().sidebarStats?.expeditionId).toBe(3)
    expect(spies.GetIngestStatus).toHaveBeenCalledTimes(2)
    expect(spies.GetSettings).toHaveBeenCalledTimes(2)

    // Selecting expedition 8 persists via SetActiveExpedition and triggers post-boot plan and stats loads.
    await act(async () => {
      await mountedHook.getResult().handleSelectExpedition(8)
    })
    expect(spies.SetActiveExpedition).toHaveBeenCalledWith(8)
    expect(mountedHook.getResult().activeExpeditionId).toBe(8)
    expect(mountedHook.getResult().plan?.expedition.name).toBe('Formidine Spur')
    expect(spies.GetSidebarStats).toHaveBeenLastCalledWith(8, expect.anything())

    // Selecting with persist: false updates activeExpeditionId without calling SetActiveExpedition again, and handleRefreshPlan reloads active plan.
    spies.SetActiveExpedition.mockClear()
    await act(async () => {
      await mountedHook.getResult().handleSelectExpedition(3, { persist: false })
    })
    expect(spies.SetActiveExpedition).not.toHaveBeenCalled()
    expect(mountedHook.getResult().activeExpeditionId).toBe(3)

    spies.GetPlan.mockClear()
    await act(async () => {
      mountedHook.getResult().handleRefreshPlan()
      await Promise.resolve()
    })
    expect(spies.GetPlan).toHaveBeenCalledWith(3)
  })
})
