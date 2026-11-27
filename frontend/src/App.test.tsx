// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { describe, expect, it } from 'vitest'
import App from './App'
import { app, expedition, stats } from './api/models'
import { installResizeObserverMock } from './test/domMocks'
import {
  makeMode,
  makeOriginRowView,
  makePlanView,
  makeSettings,
  makeSidebarStats,
} from './test/fixtures'
import { buildSampleExportData } from './test/fixtures/exportFixtures'
import { mountTestRoot, setFormControlValue } from './test/render'
import { installWailsAppMock, installWailsRuntimeMock } from './test/wailsMock'

describe('App top-level shell orchestration', () => {
  it('renders TerminalBootScreen while bootstrap RPCs are pending, runs SetupWizard when unconfigured, and transitions into workspace on completion', async () => {
    installResizeObserverMock()
    installWailsRuntimeMock()

    let setupCompleted = false
    let isFirstSettingsCall = true
    let resolveInitialSettings!: (value: app.Settings) => void

    const activeExpedition = expedition.Expedition.createFrom({
      id: 1,
      name: 'First Horizon',
      status: 'active',
    })
    const defaultIngest = app.IngestStatus.createFrom({
      filesTracked: 3,
      eventsStored: 120,
      scanning: false,
    })

    const { spies } = installWailsAppMock({
      SetLiveSyncPaused: async () => {},
      GetSettings: () => {
        if (isFirstSettingsCall) {
          isFirstSettingsCall = false
          return new Promise<app.Settings>((resolve) => {
            resolveInitialSettings = resolve
          })
        }
        return Promise.resolve(
          makeSettings({
            setupCompleted,
            activeExpeditionId: 1,
            dbPath: '/tmp/planisphere.db',
            journalDir: '/tmp/journals',
            journalDirExists: true,
          }),
        )
      },
      GetIngestStatus: async () => defaultIngest,
      GetModesState: async () =>
        app.ModesState.createFrom({ modes: [makeMode({ id: 1, name: 'Travel' })] }),
      ListExpeditions: async () => [activeExpedition],
      SetDatabasePath: async () =>
        makeSettings({
          setupCompleted: false,
          dbPath: '/tmp/planisphere.db',
          journalDir: '/tmp/journals',
          journalDirExists: true,
        }),
      SetupConfigureJournals: async () => defaultIngest,
      CompleteSetup: async () => {
        setupCompleted = true
        return makeSettings({
          setupCompleted: true,
          activeExpeditionId: 1,
          dbPath: '/tmp/planisphere.db',
        })
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
      resolveInitialSettings(
        makeSettings({
          setupCompleted: false,
          dbPath: '/tmp/planisphere.db',
          journalDir: '/tmp/journals',
          journalDirExists: true,
        }),
      )
      await Promise.resolve()
    })
    expect(mounted.container.textContent).toContain('Planisphere Setup')
    expect(mounted.container.textContent).not.toContain('EXPEDITIONS')

    // Walk through the four setup steps to finish onboarding and enter the main workspace.
    spies.GetSettings.mockImplementation(async () =>
      makeSettings({
        setupCompleted,
        activeExpeditionId: 1,
        dbPath: '/tmp/planisphere.db',
        journalDir: '/tmp/journals',
        journalDirExists: true,
      }),
    )

    await act(async () => {
      mounted.findButtonByText('NEXT').click()
      await Promise.resolve()
    })
    await act(async () => {
      mounted.findButtonByText('NEXT').click()
      await Promise.resolve()
    })
    act(() => {
      mounted.findButtonByText('NEXT').click()
    })
    await act(async () => {
      mounted.findButtonByText('FINISH SETUP').click()
      await Promise.resolve()
    })

    expect(spies.CompleteSetup).toHaveBeenCalledTimes(1)
    expect(mounted.container.textContent).toContain('First Horizon')
    expect(mounted.findButtonByText('EXPEDITIONS')).toBeTruthy()
    expect(mounted.findButtonByText('CONFIG')).toBeTruthy()
  })

  it('orchestrates active workspace modals, expedition switching, search navigation, and error banner recovery', async () => {
    installResizeObserverMock()
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

    const { spies } = installWailsAppMock({
      SetLiveSyncPaused: async () => {},
      GetSettings: async () =>
        makeSettings({
          setupCompleted: true,
          activeExpeditionId: 1,
          dbPath: '/tmp/planisphere.db',
          journalDir: '/tmp/journals',
          journalDirExists: true,
        }),
      GetIngestStatus: async () =>
        app.IngestStatus.createFrom({
          filesTracked: 10,
          eventsStored: 500,
          firstEventAt: '2026-01-01T00:00:00Z',
          lastEventAt: '2026-02-01T00:00:00Z',
        }),
      GetModesState: async () =>
        app.ModesState.createFrom({ modes: [makeMode({ id: 1, name: 'Travel' })] }),
      ListExpeditions: async () => expeditionsList,
      GetPlan: async (expeditionId: number) => getPlanForId(expeditionId),
      GetSidebarStats: async (expeditionId: number, _filter: stats.Filter) =>
        makeSidebarStats({
          expeditionId,
          status: expeditionId === 1 ? 'completed' : 'planning',
          departed: expeditionId === 1,
        }),
      SetActiveExpedition: async () => {},
      BrowseJournalDir: async () => {
        throw new Error('journal directory inaccessible')
      },
      SetJournalDir: async () =>
        makeSettings({
          setupCompleted: true,
          activeExpeditionId: 1,
          journalDir: '/tmp/journals',
          journalDirExists: true,
        }),
      GetExpeditionExportData: async () => buildSampleExportData(),
      GetStartCandidates: async () =>
        app.StartCandidates.createFrom({
          systemName: 'Colonia',
          visits: [],
        }),
      SearchWaypoints: async () =>
        expedition.SearchResponse.createFrom({
          waypoints: [
            {
              expeditionId: 2,
              expeditionName: 'Outer Arm Draft',
              expeditionStatus: 'planning',
              rowId: 201,
              waypointNumber: 1,
              systemName: 'Colonia',
              visitState: 'future',
            },
          ],
          nonWaypointVisits: [],
        }),
    })

    const mounted = mountTestRoot(createElement(App))
    await act(async () => {
      await Promise.resolve()
    })

    expect(mounted.container.textContent).toContain('Deep Core Run')

    // Open SettingsModal via CONFIG, surface and dismiss an RPC error banner, and close it.
    await act(async () => {
      mounted.findButtonByText('CONFIG').click()
      await Promise.resolve()
    })
    const settingsDialog = mounted.container.querySelector(
      '[aria-label="Terminal Configuration"]',
    ) as HTMLElement
    expect(settingsDialog).not.toBeNull()

    await act(async () => {
      mounted.findButtonByText('BROWSE...', { index: 0 }).click()
      await Promise.resolve()
    })
    expect(mounted.container.textContent).toContain('journal directory inaccessible')

    await act(async () => {
      mounted.findButtonByText('APPLY', { index: 0 }).click()
      await Promise.resolve()
    })
    expect(mounted.container.textContent).not.toContain('journal directory inaccessible')

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
      await Promise.resolve()
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
      await Promise.resolve()
    })
    expect(spies.SetActiveExpedition).toHaveBeenCalledWith(2)
    expect(mounted.container.textContent).toContain('Outer Arm Draft')

    await act(async () => {
      mounted.findButtonByText('ENGAGE').click()
      await Promise.resolve()
    })
    const startDialog = mounted.container.querySelector('[role="dialog"]') as HTMLElement
    expect(startDialog).not.toBeNull()
    act(() => {
      ;(startDialog.querySelector('button') as HTMLButtonElement).click()
    })
    expect(mounted.container.querySelector('[role="dialog"]')).toBeNull()
  })

  it('skips All Time sidebar telemetry while collapsed or in Expedition scope and lazily loads it on clicking All Time', async () => {
    installResizeObserverMock()
    installWailsRuntimeMock()
    window.localStorage.setItem('planisphere_sidebar_collapsed', 'true')

    try {
      const { spies } = installWailsAppMock({
        SetLiveSyncPaused: async () => {},
        GetSettings: async () =>
          makeSettings({
            setupCompleted: true,
            activeExpeditionId: 1,
            dbPath: '/tmp/planisphere.db',
            journalDir: '/tmp/journals',
            journalDirExists: true,
          }),
        GetIngestStatus: async () => app.IngestStatus.createFrom({ scanning: false }),
        GetModesState: async () =>
          app.ModesState.createFrom({ modes: [makeMode({ id: 1, name: 'Travel' })] }),
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
        await Promise.resolve()
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
        await Promise.resolve()
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
})
