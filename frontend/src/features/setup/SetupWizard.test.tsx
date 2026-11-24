// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { app } from '../../api/models'

const mockSetDatabasePath = vi.fn()
const mockSetupConfigureJournals = vi.fn()
const mockGetSettings = vi.fn()
const mockGetIngestStatus = vi.fn()
const mockSetOnlineLookups = vi.fn()
const mockRefreshGEC = vi.fn()

vi.mock('../../api/backend', () => ({
  BrowseDBPath: vi.fn(),
  BrowseEDDiscoveryDB: vi.fn(),
  BrowseJournalDir: vi.fn(),
  CompleteSetup: vi.fn(),
  GetIngestStatus: (...args: unknown[]) => mockGetIngestStatus(...args),
  GetSettings: (...args: unknown[]) => mockGetSettings(...args),
  ImportEDDiscoveryHistory: vi.fn(),
  RefreshGEC: (...args: unknown[]) => mockRefreshGEC(...args),
  SetCommanderEnabled: vi.fn(),
  SetDatabasePath: (...args: unknown[]) => mockSetDatabasePath(...args),
  SetOnlineLookups: (...args: unknown[]) => mockSetOnlineLookups(...args),
  SetupConfigureJournals: (...args: unknown[]) => mockSetupConfigureJournals(...args),
}))

import { SetupWizardModal } from './SetupWizard'

;(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

describe('SetupWizardModal async edge safety', () => {
  afterEach(() => vi.clearAllMocks())

  it('rolls back onlineLookups when SetOnlineLookups rejects and blocks Back, close, and checkboxes during GEC sync', async () => {
    const initialSettings = app.Settings.createFrom({
      dbPath: '/tmp/planisphere.db',
      defaultDbPath: '/tmp/planisphere.db',
      journalDir: '/tmp/journals',
      defaultJournalDir: '/tmp/journals',
      journalDirExists: true,
      onlineLookups: true,
      commanders: [{ name: 'Jameson', enabled: true, eventCount: 42 }],
    })
    const initialIngest = app.IngestStatus.createFrom({ filesTracked: 1, eventsStored: 42 })
    mockSetDatabasePath.mockResolvedValue(initialSettings)
    mockGetIngestStatus.mockResolvedValue(initialIngest)
    mockSetupConfigureJournals.mockResolvedValue(initialIngest)
    mockGetSettings.mockResolvedValue(initialSettings)
    mockSetOnlineLookups.mockRejectedValueOnce(new Error('settings write failed'))

    let resolveGecSync!: (count: number) => void
    mockRefreshGEC.mockImplementationOnce(
      () => new Promise<number>((resolve) => { resolveGecSync = resolve }),
    )

    const onClose = vi.fn()
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const findButton = (label: string) =>
      Array.from(container.querySelectorAll('button')).find((button) =>
        button.textContent?.includes(label),
      ) as HTMLButtonElement

    try {
      act(() => {
        root.render(
          createElement(SetupWizardModal, {
            settings: initialSettings,
            ingestStatus: initialIngest,
            onComplete: () => {},
            onClose,
          }),
        )
      })
      await act(async () => { findButton('NEXT').click(); await Promise.resolve() })
      await act(async () => { findButton('NEXT').click(); await Promise.resolve() })
      act(() => findButton('NEXT').click())
      expect(container.textContent).toContain('Step 4 of 4')

      const checkboxes = () =>
        Array.from(container.querySelectorAll('input[type="checkbox"]')) as HTMLInputElement[]
      await act(async () => { checkboxes()[1].click(); await Promise.resolve() })
      expect(mockSetOnlineLookups).toHaveBeenCalledWith(false)
      expect(checkboxes()[1].checked).toBe(true)
      expect(container.textContent).toContain('settings write failed')

      act(() => findButton('SYNC CATALOG NOW').click())
      const closeHeaderButton = container.querySelector('button[title="Close setup wizard"]') as HTMLButtonElement
      expect(findButton('SYNCING...').disabled).toBe(true)
      expect(findButton('BACK').disabled).toBe(true)
      expect(closeHeaderButton.disabled).toBe(true)
      expect(checkboxes()[0].disabled).toBe(true)
      expect(checkboxes()[1].disabled).toBe(true)

      act(() => { findButton('BACK').click(); closeHeaderButton.click() })
      expect(container.textContent).toContain('Step 4 of 4')
      expect(onClose).not.toHaveBeenCalled()

      act(() => root.unmount())
      container.remove()
      await act(async () => { resolveGecSync(1200); await Promise.resolve() })
    } finally {
      if (container.isConnected) {
        act(() => root.unmount())
        container.remove()
      }
    }
  })
})
