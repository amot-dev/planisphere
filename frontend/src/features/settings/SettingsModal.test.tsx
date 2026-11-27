// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { app, type EddImportProgress } from '../../api/models'
import { makeMode, makeSettings } from '../../test/fixtures'
import { mountTestRoot } from '../../test/render'
import { installWailsAppMock } from '../../test/wailsMock'
import { SettingsModal } from './SettingsModal'

describe('SettingsModal', () => {
  it('renders all configuration sections, triggers close callback, and disables dismissal while busy', () => {
    const onClose = vi.fn()
    const settings = makeSettings({
      dbPath: '/data/planisphere.db',
      journalDir: '/journals',
      journalDirExists: true,
    })
    const ingestStatus = app.IngestStatus.createFrom({
      filesTracked: 12,
      eventsStored: 950,
      scanning: false,
    })
    const modesState = app.ModesState.createFrom({
      modes: [makeMode({ id: 1, name: 'Travel' })],
    })

    const mounted = mountTestRoot(
      createElement(SettingsModal, {
        settings,
        ingestStatus,
        modesState,
        eddProgress: null,
        onClose,
      }),
    )

    const dialog = mounted.container.querySelector('[role="dialog"]') as HTMLElement
    expect(dialog).not.toBeNull()
    expect(dialog.getAttribute('aria-label')).toBe('Terminal Configuration')

    const renderedText = mounted.container.textContent ?? ''
    expect(renderedText).toContain('Journal Directory')
    expect(renderedText).toContain('Database and Event Archive')
    expect(renderedText).toContain('Fleet Carrier Cost Valuation')
    expect(renderedText).toContain('Historical Journal Import (EDDiscovery)')
    expect(renderedText).toContain('Online Services')
    expect(mounted.queryButtonByText('RUN SETUP WIZARD...')).toBeNull()

    const headerCloseButton = dialog.querySelector('button') as HTMLButtonElement
    act(() => {
      headerCloseButton.click()
    })
    expect(onClose).toHaveBeenCalledTimes(1)

    // Active EDDiscovery import or journal scanning disables modal dismissal.
    const activeEddProgress: EddImportProgress = {
      active: true,
      stage: 'Importing events',
      current: 50,
      total: 100,
      percent: 50,
      files: 2,
    }
    mounted.rerender(
      createElement(SettingsModal, {
        settings,
        ingestStatus: app.IngestStatus.createFrom({ scanning: true }),
        modesState,
        eddProgress: activeEddProgress,
        onClose,
      }),
    )
    const updatedDialog = mounted.container.querySelector('[role="dialog"]') as HTMLElement
    const updatedCloseButton = updatedDialog.querySelector('button') as HTMLButtonElement
    expect(updatedCloseButton.disabled).toBe(true)
  })

  it('surfaces and clears error banners propagated from child configuration sections', async () => {
    const settings = makeSettings({
      journalDir: '/journals',
      journalDirExists: true,
    })
    installWailsAppMock({
      BrowseJournalDir: async () => {
        throw new Error('folder picker failed')
      },
      SetJournalDir: async (selectedDirectory: string) =>
        makeSettings({ journalDir: selectedDirectory, journalDirExists: true }),
    })

    const mounted = mountTestRoot(
      createElement(SettingsModal, {
        settings,
        ingestStatus: null,
        modesState: null,
        onClose: () => {},
      }),
    )

    await act(async () => {
      mounted.findButtonByText('BROWSE...', { index: 0 }).click()
      await Promise.resolve()
    })
    expect(mounted.container.textContent).toContain('folder picker failed')

    await act(async () => {
      mounted.findButtonByText('APPLY', { index: 0 }).click()
      await Promise.resolve()
    })
    expect(mounted.container.textContent).not.toContain('folder picker failed')
  })
})
