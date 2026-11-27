// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { describe, expect, it } from 'vitest'
import App from './App'
import { app, expedition, stats } from './api/models'
import { createDeferred, flushMicrotasks } from './test/async'
import { installResizeObserverMock } from './test/domMocks'
import {
  makeMode,
  makeOriginRowView,
  makePlanView,
  makeSettings,
  makeSidebarStats,
  makeWaypointDetails,
} from './test/fixtures'
import { buildSampleExportData } from './test/fixtures/exportFixtures'
import { mountTestRoot, setFormControlValue } from './test/render'
import {
  installWailsAppMock,
  installWailsRuntimeMock,
  type WailsAppMockOverrides,
} from './test/wailsMock'

function makeConfiguredSettings(setupCompleted = true): app.Settings {
  return makeSettings({
    setupCompleted,
    activeExpeditionId: 1,
    dbPath: '/tmp/planisphere.db',
    journalDir: '/tmp/journals',
    journalDirExists: true,
  })
}

function installWorkspaceAppMocks(overrides: WailsAppMockOverrides) {
  installResizeObserverMock()
  return installWailsAppMock({
    SetLiveSyncPaused: async () => {},
    GetSettings: async () => makeConfiguredSettings(true),
    GetIngestStatus: async () => app.IngestStatus.createFrom({ scanning: false }),
    GetModesState: async () =>
      app.ModesState.createFrom({ modes: [makeMode({ id: 1, name: 'Travel' })] }),
    ...overrides,
  })
}

describe('App top-level shell orchestration', () => {
  it('renders TerminalBootScreen while bootstrap RPCs are pending, runs SetupWizard when unconfigured, and transitions into workspace on completion', async () => {
    const runtimeMock = installWailsRuntimeMock()
    const initialSettingsDeferred = createDeferred<app.Settings>()

    let setupCompleted = false
    let journalsConfigured = false
    let isFirstSettingsCall = true

    const activeExpedition = expedition.Expedition.createFrom({
      id: 1,
      name: 'First Horizon',
      status: 'active',
    })
    const emptyBootIngest = app.IngestStatus.createFrom({
      filesTracked: 0,
      eventsStored: 0,
      scanning: false,
      archiveLoaded: true,
    })
    const scannedIngest = app.IngestStatus.createFrom({
      filesTracked: 3,
      eventsStored: 120,
      firstEventAt: '2026-01-01T00:00:00Z',
      lastEventAt: '2026-01-02T00:00:00Z',
      scanning: false,
      archiveLoaded: true,
    })

    const { spies } = installWorkspaceAppMocks({
      GetSettings: () => {
        if (isFirstSettingsCall) {
          isFirstSettingsCall = false
          return initialSettingsDeferred.promise
        }
        return Promise.resolve(makeConfiguredSettings(setupCompleted))
      },
      GetIngestStatus: async () => (journalsConfigured ? scannedIngest : emptyBootIngest),
      ListExpeditions: async () => [activeExpedition],
      SetDatabasePath: async () => makeConfiguredSettings(false),
      SetupConfigureJournals: async () => {
        journalsConfigured = true
        return scannedIngest
      },
      CompleteSetup: async () => {
        setupCompleted = true
        return makeConfiguredSettings(true)
      },
      GetPlan: async (expeditionId: number) =>
        makePlanView({
          expedition: { id: expeditionId, name: 'First Horizon', status: 'active' },
          rows: [makeOriginRowView()],
        }),
      GetSidebarStats: async (expeditionId: number, _filter: stats.Filter) =>
        makeSidebarStats({ expeditionId, status: 'active', departed: true }),
    })

    const mounted = mountTestRoot(createElement(App))
    expect(mounted.container.textContent).toContain('Initializing Planisphere Terminal...')

    // Resolve initial GetSettings with setupCompleted = false to enter SetupWizardModal.
    await act(async () => {
      initialSettingsDeferred.resolve(makeConfiguredSettings(false))
      await flushMicrotasks()
    })
    expect(mounted.container.textContent).toContain('Planisphere Setup')
    expect(mounted.container.textContent).not.toContain('EXPEDITIONS')

    // Walk through the four setup steps to finish onboarding and enter the main workspace.
    spies.GetSettings.mockImplementation(async () => makeConfiguredSettings(setupCompleted))

    await act(async () => {
      mounted.findButtonByText('NEXT').click()
      await flushMicrotasks()
    })
    await act(async () => {
      mounted.findButtonByText('NEXT').click()
      await flushMicrotasks()
    })
    expect(mounted.container.textContent).toContain('3 files')
    expect(mounted.container.textContent).toContain('120 events')

    // Live ingest notification during HistoricalImportStep invokes full GetIngestStatus and preserves the scanned summary.
    const ingestCallsBeforeLiveDuringSetup = spies.GetIngestStatus.mock.calls.length
    await act(async () => {
      runtimeMock.emitDataChanged({ domains: ['ingest'] })
      await flushMicrotasks()
    })
    expect(spies.GetIngestStatus).toHaveBeenCalledTimes(ingestCallsBeforeLiveDuringSetup + 1)
    expect(mounted.container.textContent).toContain('3 files')
    expect(mounted.container.textContent).toContain('120 events')

    act(() => {
      mounted.findButtonByText('NEXT').click()
    })
    await act(async () => {
      mounted.findButtonByText('FINISH SETUP').click()
      await flushMicrotasks()
    })

    expect(spies.CompleteSetup).toHaveBeenCalledTimes(1)
    expect(mounted.container.textContent).toContain('First Horizon')
    expect(mounted.findButtonByText('EXPEDITIONS')).toBeTruthy()
    expect(mounted.findButtonByText('CONFIG')).toBeTruthy()
  })

  it('orchestrates active workspace modals, expedition switching, and search modal dismissal', async () => {
    installWailsRuntimeMock()

    const expeditionsList = [
      expedition.Expedition.createFrom({ id: 1, name: 'Deep Core Run', status: 'completed' }),
      expedition.Expedition.createFrom({ id: 2, name: 'Outer Arm Draft', status: 'planning' }),
    ]

    const getPlanForId = (expeditionId: number) =>
      expeditionId === 1
        ? makePlanView({
            expedition: { id: 1, name: 'Deep Core Run', status: 'completed' },
            rows: [makeOriginRowView({ row: { id: 101, expeditionId: 1, systemName: 'Sol' } })],
            waypointCount: 1,
            visitedWaypointCount: 1,
          })
        : makePlanView({
            expedition: { id: 2, name: 'Outer Arm Draft', status: 'planning' },
            rows: [
              makeOriginRowView({
                visitState: 'future',
                row: { id: 201, expeditionId: 2, systemName: 'Colonia' },
              }),
            ],
            waypointCount: 1,
            visitedWaypointCount: 0,
          })

    const { spies } = installWorkspaceAppMocks({
      GetIngestStatus: async () =>
        app.IngestStatus.createFrom({
          filesTracked: 10,
          eventsStored: 500,
          firstEventAt: '2026-01-01T00:00:00Z',
          lastEventAt: '2026-02-01T00:00:00Z',
        }),
      ListExpeditions: async () => expeditionsList,
      GetPlan: async (expeditionId: number) => getPlanForId(expeditionId),
      GetSidebarStats: async (expeditionId: number, _filter: stats.Filter) =>
        makeSidebarStats({
          expeditionId,
          status: expeditionId === 1 ? 'completed' : 'planning',
          departed: expeditionId === 1,
        }),
      SetActiveExpedition: async () => {},
      GetExpeditionExportData: async () => buildSampleExportData(),
      GetStartCandidates: async () =>
        app.StartCandidates.createFrom({
          systemName: 'Colonia',
          visits: [],
        }),
    })

    const mounted = mountTestRoot(createElement(App))
    await act(async () => {
      await flushMicrotasks()
    })

    expect(mounted.container.textContent).toContain('Deep Core Run')

    // Open and close SettingsModal via CONFIG.
    await act(async () => {
      mounted.findButtonByText('CONFIG').click()
      await flushMicrotasks()
    })
    const settingsDialog = mounted.container.querySelector(
      '[aria-label="Terminal Configuration"]',
    ) as HTMLElement
    expect(settingsDialog).not.toBeNull()
    act(() => {
      ;(settingsDialog.querySelector('button') as HTMLButtonElement).click()
    })
    expect(mounted.container.querySelector('[role="dialog"]')).toBeNull()

    // Open and close ExpeditionManageModal via EXPEDITIONS button.
    act(() => {
      mounted.findButtonByText('EXPEDITIONS').click()
    })
    const expeditionsDialog = mounted.container.querySelector('[role="dialog"]') as HTMLElement
    expect(expeditionsDialog).not.toBeNull()
    act(() => {
      ;(expeditionsDialog.querySelector('button') as HTMLButtonElement).click()
    })
    expect(mounted.container.querySelector('[role="dialog"]')).toBeNull()

    // Open and close ExportExpeditionModal on completed expedition #1.
    await act(async () => {
      mounted.findButtonByText('EXPORT').click()
      await flushMicrotasks()
    })
    const exportDialog = mounted.container.querySelector(
      '[aria-label="Export Studio"]',
    ) as HTMLElement
    expect(exportDialog).not.toBeNull()
    act(() => {
      ;(exportDialog.querySelector('button') as HTMLButtonElement).click()
    })
    expect(mounted.container.querySelector('[aria-label="Export Studio"]')).toBeNull()

    // Open WaypointSearchModal and close via Escape.
    const searchButton = mounted.container.querySelector(
      'button[aria-label="Search waypoints and visit history"]',
    ) as HTMLButtonElement
    act(() => searchButton.click())
    expect(
      mounted.container.querySelector('input[aria-label="Search waypoints and visit history"]'),
    ).not.toBeNull()
    act(() => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    })
    expect(
      mounted.container.querySelector('input[aria-label="Search waypoints and visit history"]'),
    ).toBeNull()

    // Switch active expedition via top-bar selector and open StartExpeditionModal on planning expedition #2.
    const expeditionSelect = mounted.container.querySelector(
      'select[title="Select active expedition"]',
    ) as HTMLSelectElement
    await act(async () => {
      setFormControlValue(expeditionSelect, '2')
      await flushMicrotasks()
    })
    expect(spies.SetActiveExpedition).toHaveBeenCalledWith(2)
    expect(mounted.container.textContent).toContain('Outer Arm Draft')

    await act(async () => {
      mounted.findButtonByText('ENGAGE').click()
      await flushMicrotasks()
    })
    const startDialog = mounted.container.querySelector('[role="dialog"]') as HTMLElement
    expect(startDialog).not.toBeNull()
    act(() => {
      ;(startDialog.querySelector('button') as HTMLButtonElement).click()
    })
    expect(mounted.container.querySelector('[role="dialog"]')).toBeNull()
  })

  it('skips All Time sidebar telemetry while collapsed or in Expedition scope and lazily loads it on clicking All Time', async () => {
    installWailsRuntimeMock()
    window.localStorage.setItem('planisphere_sidebar_collapsed', 'true')

    try {
      const { spies } = installWorkspaceAppMocks({
        ListExpeditions: async () => [
          expedition.Expedition.createFrom({ id: 1, name: 'Lazy Telemetry Run', status: 'active' }),
        ],
        GetPlan: async (expeditionId: number) =>
          makePlanView({
            expedition: { id: expeditionId, name: 'Lazy Telemetry Run', status: 'active' },
            rows: [makeOriginRowView()],
          }),
        GetSidebarStats: async (
          expeditionId: number,
          _filter: stats.Filter,
          includeAllTime: boolean,
        ) =>
          makeSidebarStats({
            expeditionId,
            status: 'active',
            started: true,
            departed: true,
            duration: '3d 4h',
            allTimeLoaded: includeAllTime,
            careerStart: includeAllTime ? '2024-01-01T00:00:00Z' : '',
            careerStartGalactic: includeAllTime ? '3310-01-01 00:00 UTC' : '',
            careerDuration: includeAllTime ? '2y 30d' : '',
            expedition: { jumps: 9, distanceLy: 450 },
            allJournals: includeAllTime ? { jumps: 512, distanceLy: 25600 } : { jumps: 0 },
          }),
      })

      const mounted = mountTestRoot(createElement(App))
      await act(async () => {
        await flushMicrotasks()
      })

      // Booting with collapsed sidebar still queries Expedition stats with includeAllTime=false.
      expect(spies.GetSidebarStats).toHaveBeenCalledTimes(1)
      expect(spies.GetSidebarStats).toHaveBeenLastCalledWith(1, expect.anything(), false)

      const expandButton = mounted.container.querySelector(
        'button[title="Expand Telemetry Sidebar"]',
      ) as HTMLButtonElement
      act(() => {
        expandButton.click()
      })
      expect(spies.GetSidebarStats).toHaveBeenCalledTimes(1)
      expect(mounted.container.textContent).toContain('Expedition Duration')
      expect(mounted.container.textContent).toContain('3d 4h')

      // Clicking All Time keeps the sidebar rendered and triggers GetSidebarStats(..., true).
      await act(async () => {
        mounted.findButtonByText('All Time').click()
        await flushMicrotasks()
      })
      expect(spies.GetSidebarStats).toHaveBeenCalledTimes(2)
      expect(spies.GetSidebarStats).toHaveBeenLastCalledWith(1, expect.anything(), true)
      expect(mounted.container.textContent).toContain('Career Duration')
      expect(mounted.container.textContent).toContain('2y 30d')
      expect(mounted.container.textContent).toContain('25,600.0')
    } finally {
      window.localStorage.removeItem('planisphere_sidebar_collapsed')
    }
  })

  it('keeps open active-leg drawers idle across unchanged telemetryRevision, refreshes archive counts on opening CONFIG, and refreshes drawers when telemetryRevision advances', async () => {
    const runtimeMock = installWailsRuntimeMock()
    let currentTelemetryRevision = 1
    let fullArchiveEvents = 400
    let detailsCallCount = 0

    const activePlan = makePlanView({
      expedition: { id: 1, name: 'Telemetry Gate Run', status: 'active' },
      rows: [
        makeOriginRowView({
          row: {
            id: 101,
            expeditionId: 1,
            systemName: 'Sol',
            visitedAt: '2026-01-01T10:00:00Z',
            departedAt: '2026-01-01T10:15:00Z',
          },
          drawerTo: '2026-01-01T10:15:00Z',
        }),
        expedition.RowView.createFrom({
          row: {
            id: 102,
            expeditionId: 1,
            rowType: 'waypoint',
            systemName: 'Alpha Centauri',
            seq: 1,
            visitedAt: '2026-01-01T11:00:00Z',
            departedAt: '',
          },
          number: 2,
          visitState: 'visited',
          drawerFrom: '2026-01-01T10:15:00Z',
          permissions: { hasDrawer: true },
        }),
      ],
      lastVisitedIndex: 1,
      visitedWaypointCount: 2,
      waypointCount: 2,
    })

    const { spies } = installWorkspaceAppMocks({
      GetIngestStatus: async () =>
        app.IngestStatus.createFrom({
          filesTracked: 8,
          eventsStored: fullArchiveEvents,
          currentSystem: 'Alpha Centauri',
          currentShip: 'Mandalay',
          closedWindowRevision: 1,
          telemetryRevision: currentTelemetryRevision,
          archiveLoaded: true,
        }),
      GetLiveIngestStatus: async () =>
        app.IngestStatus.createFrom({
          currentSystem: 'Alpha Centauri',
          currentShip: 'Mandalay',
          closedWindowRevision: 1,
          telemetryRevision: currentTelemetryRevision,
          archiveLoaded: false,
        }),
      ListExpeditions: async () => [
        expedition.Expedition.createFrom({ id: 1, name: 'Telemetry Gate Run', status: 'active' }),
      ],
      GetPlan: async () => activePlan,
      GetSidebarStats: async (expeditionId: number) =>
        makeSidebarStats({ expeditionId, status: 'active', departed: true }),
      GetWaypointDetails: async (rowId: number) => {
        detailsCallCount += 1
        return makeWaypointDetails({
          rowId,
          kind: 'leg',
          number: 2,
          from: '2026-01-01T10:15:00Z',
          to: '',
          arrivedAt: '2026-01-01T11:00:00Z',
          inSystem: true,
          metrics: { jumps: detailsCallCount === 1 ? 3 : 4 },
        })
      },
    })

    const mounted = mountTestRoot(createElement(App))
    await act(async () => {
      await flushMicrotasks()
    })

    const drawerToggles = mounted.container.querySelectorAll<HTMLButtonElement>(
      'button[aria-label="Toggle per-waypoint leg telemetry drawer"]',
    )
    await act(async () => {
      drawerToggles[1].click()
      await flushMicrotasks()
    })
    expect(spies.GetWaypointDetails).toHaveBeenCalledTimes(1)

    // Live ingest notification with unchanged telemetryRevision uses GetLiveIngestStatus and keeps open active-leg drawer idle.
    await act(async () => {
      runtimeMock.emitDataChanged({ domains: ['ingest'] })
      await flushMicrotasks()
    })
    expect(spies.GetLiveIngestStatus).toHaveBeenCalledTimes(1)
    expect(spies.GetIngestStatus).toHaveBeenCalledTimes(1)
    expect(spies.GetWaypointDetails).toHaveBeenCalledTimes(1)

    // Silent ambient events increased SQLite archive count to 425 without advancing telemetryRevision; opening CONFIG fetches full GetIngestStatus without re-fetching the drawer.
    fullArchiveEvents = 425
    await act(async () => {
      mounted.findButtonByText('CONFIG').click()
      await flushMicrotasks()
    })
    expect(spies.GetIngestStatus).toHaveBeenCalledTimes(2)
    expect(mounted.container.textContent).toContain('425')
    expect(spies.GetWaypointDetails).toHaveBeenCalledTimes(1)

    // Close SettingsModal, then emit a live telemetry event advancing telemetryRevision to 2: GetLiveIngestStatus runs and refreshes the open active-leg drawer.
    const settingsDialog = mounted.container.querySelector(
      '[aria-label="Terminal Configuration"]',
    ) as HTMLElement
    act(() => {
      ;(settingsDialog.querySelector('button') as HTMLButtonElement).click()
    })
    currentTelemetryRevision = 2
    await act(async () => {
      runtimeMock.emitDataChanged({ domains: ['ingest'] })
      await flushMicrotasks()
    })
    expect(spies.GetLiveIngestStatus).toHaveBeenCalledTimes(2)
    expect(spies.GetWaypointDetails).toHaveBeenCalledTimes(2)
  })
})
