// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import type { EddImportProgress } from '../../api/models'
import { makeSettings } from '../../test/fixtures'
import { mountTestRoot, setFormControlValue } from '../../test/render'
import { installWailsAppMock } from '../../test/wailsMock'
import { EDDiscoveryImportSection } from './EDDiscoveryImportSection'

describe('EDDiscoveryImportSection', () => {
  it('selects database path via BrowseEDDiscoveryDB, runs ImportEDDiscoveryHistory with busy state, and handles errors', async () => {
    const onSetError = vi.fn()
    let resolveImport!: (importedCount: number) => void

    const { spies } = installWailsAppMock({
      BrowseEDDiscoveryDB: async () => '/backups/EDDUser.sqlite',
      ImportEDDiscoveryHistory: () =>
        new Promise<number>((resolve) => {
          resolveImport = resolve
        }),
    })

    const mounted = mountTestRoot(
      createElement(EDDiscoveryImportSection, {
        settings: makeSettings({ edDiscoveryDbPath: '/initial/EDDUser.sqlite' }),
        eddProgress: null,
        onSetError,
      }),
    )

    const databaseInput = mounted.container.querySelector('input[type="text"]') as HTMLInputElement
    expect(databaseInput.value).toBe('/initial/EDDUser.sqlite')

    // Cancelled browse keeps existing path; confirmed browse updates input.
    spies.BrowseEDDiscoveryDB.mockResolvedValueOnce('')
    await act(async () => {
      mounted.findButtonByText('BROWSE...').click()
      await Promise.resolve()
    })
    expect(databaseInput.value).toBe('/initial/EDDUser.sqlite')

    await act(async () => {
      mounted.findButtonByText('BROWSE...').click()
      await Promise.resolve()
    })
    expect(databaseInput.value).toBe('/backups/EDDUser.sqlite')

    // Starting import disables button and displays preparing progress state until resolved.
    act(() => {
      mounted.findButtonByText('IMPORT').click()
    })
    const busyImportButton = mounted.findButtonByText('IMPORTING...')
    expect(busyImportButton.disabled).toBe(true)
    expect(mounted.container.textContent).toContain('Reading database...')
    expect(mounted.container.textContent).toContain('Preparing...')

    await act(async () => {
      resolveImport(2450)
      await Promise.resolve()
    })
    expect(spies.ImportEDDiscoveryHistory).toHaveBeenCalledWith('/backups/EDDUser.sqlite')
    expect(mounted.findButtonByText('IMPORT').disabled).toBe(false)
    expect(mounted.container.textContent).toContain(
      'Imported 2,450 historical journal events from EDDiscovery.',
    )

    // Manual input edit and rejected RPCs forward error messages to onSetError.
    act(() => setFormControlValue(databaseInput, '/corrupt/EDDUser.sqlite'))
    spies.BrowseEDDiscoveryDB.mockRejectedValueOnce(new Error('picker unavailable'))
    spies.ImportEDDiscoveryHistory.mockRejectedValueOnce(new Error('sqlite open failed'))

    await act(async () => {
      mounted.findButtonByText('BROWSE...').click()
      await Promise.resolve()
    })
    expect(onSetError).toHaveBeenLastCalledWith('Error: picker unavailable')

    await act(async () => {
      mounted.findButtonByText('IMPORT').click()
      await Promise.resolve()
    })
    expect(spies.ImportEDDiscoveryHistory).toHaveBeenLastCalledWith('/corrupt/EDDUser.sqlite')
    expect(onSetError).toHaveBeenLastCalledWith('Error: sqlite open failed')
    expect(mounted.findButtonByText('IMPORT').disabled).toBe(false)
  })

  it('renders live EddImportProgress stage, formatted event counts, percentage bar, and disabled button state', () => {
    const liveProgress: EddImportProgress = {
      active: true,
      stage: 'Scanning JournalEntries',
      current: 1600,
      total: 2500,
      percent: 64,
      files: 18,
    }

    const mounted = mountTestRoot(
      createElement(EDDiscoveryImportSection, {
        settings: makeSettings({ edDiscoveryDbPath: '/backups/EDDUser.sqlite' }),
        eddProgress: liveProgress,
        onSetError: () => {},
      }),
    )

    const importButton = mounted.findButtonByText('IMPORTING...')
    expect(importButton.disabled).toBe(true)
    expect(mounted.container.textContent).toContain('Scanning JournalEntries')
    expect(mounted.container.textContent).toContain('64% (1,600 / 2,500 events)')

    const progressFill = mounted.container.querySelector('.h-full.bg-ed-cyan') as HTMLDivElement
    expect(progressFill.style.width).toBe('64%')
  })
})
