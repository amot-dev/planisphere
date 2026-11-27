// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { app } from '../../api/models'
import { makeSettings } from '../../test/fixtures'
import { mountTestRoot } from '../../test/render'
import { installWailsAppMock } from '../../test/wailsMock'
import { DatabaseArchiveSection } from './DatabaseArchiveSection'

describe('DatabaseArchiveSection', () => {
  it('displays active database path and archive counters, guards rescan while scanning, and commits debounce updates', async () => {
    const onSetError = vi.fn()
    let resolveRescan!: () => void

    const { spies } = installWailsAppMock({
      RescanJournals: () =>
        new Promise<void>((resolve) => {
          resolveRescan = resolve
        }),
      SetJournalDebounceMs: async (milliseconds: number) =>
        makeSettings({ journalDebounceMs: milliseconds }),
    })

    const settings = makeSettings({
      dbPath: '/var/lib/planisphere/commander.db',
      journalDebounceMs: 1500,
      defaultJournalDebounceMs: 1500,
    })
    const activeIngest = app.IngestStatus.createFrom({
      filesTracked: 48,
      eventsStored: 18420,
      watcherActive: true,
      scanning: false,
    })

    const mounted = mountTestRoot(
      createElement(DatabaseArchiveSection, {
        settings,
        ingestStatus: activeIngest,
        onSetError,
      }),
    )

    const renderedText = mounted.container.textContent ?? ''
    expect(renderedText).toContain('/var/lib/planisphere/commander.db')
    expect(renderedText).toContain('48')
    expect(renderedText).toContain('18,420')
    expect(renderedText).toContain('ACTIVE')

    // Triggering RescanJournals disables button until completion.
    act(() => {
      mounted.findButtonByText('RESCAN JOURNALS').click()
    })
    expect(mounted.findButtonByText('RESCANNING...').disabled).toBe(true)

    await act(async () => {
      resolveRescan()
      await Promise.resolve()
    })
    expect(spies.RescanJournals).toHaveBeenCalledTimes(1)
    expect(mounted.findButtonByText('RESCAN JOURNALS').disabled).toBe(false)

    // Committing a debounce preset invokes SetJournalDebounceMs.
    await act(async () => {
      mounted.findButtonByText('5s').click()
      await Promise.resolve()
    })
    expect(spies.SetJournalDebounceMs).toHaveBeenCalledWith(5000)

    // External scanning state disables rescan button and standby state renders when watcher is inactive.
    mounted.rerender(
      createElement(DatabaseArchiveSection, {
        settings,
        ingestStatus: app.IngestStatus.createFrom({
          filesTracked: 0,
          eventsStored: 0,
          watcherActive: false,
          scanning: true,
        }),
        onSetError,
      }),
    )
    expect(mounted.container.textContent).toContain('STANDBY')
    expect(mounted.findButtonByText('RESCANNING...').disabled).toBe(true)

    // Rejections from RescanJournals and SetJournalDebounceMs propagate via onSetError.
    mounted.rerender(
      createElement(DatabaseArchiveSection, {
        settings,
        ingestStatus: null,
        onSetError,
      }),
    )
    spies.RescanJournals.mockRejectedValueOnce(new Error('rescan failed'))
    spies.SetJournalDebounceMs.mockRejectedValueOnce(new Error('debounce save failed'))

    await act(async () => {
      mounted.findButtonByText('RESCAN JOURNALS').click()
      await Promise.resolve()
    })
    expect(onSetError).toHaveBeenLastCalledWith('Error: rescan failed')

    await act(async () => {
      mounted.findButtonByText('15s').click()
      await Promise.resolve()
    })
    expect(onSetError).toHaveBeenLastCalledWith('Error: debounce save failed')
  })
})
