// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { describe, expect, it } from 'vitest'
import { app } from '../../api/models'
import { makeSettings } from '../../test/fixtures'
import { mountTestRoot } from '../../test/render'
import { installWailsAppMock } from '../../test/wailsMock'
import { SetupWizardModal } from './SetupWizard'

describe('SetupWizardModal async edge safety', () => {
  it('omits modal close control, rolls back onlineLookups when SetOnlineLookups rejects, and blocks Back and checkboxes during GEC sync', async () => {
    const initialSettings = makeSettings({
      dbPath: '/tmp/planisphere.db',
      defaultDbPath: '/tmp/planisphere.db',
      journalDir: '/tmp/journals',
      defaultJournalDir: '/tmp/journals',
      journalDirExists: true,
      onlineLookups: true,
      commanders: [{ name: 'Jameson', enabled: true, eventCount: 42 }],
    })
    const initialIngest = app.IngestStatus.createFrom({ filesTracked: 1, eventsStored: 42 })

    let resolveGecSync!: (count: number) => void
    const { spies } = installWailsAppMock({
      SetDatabasePath: async () => initialSettings,
      GetIngestStatus: async () => initialIngest,
      SetupConfigureJournals: async () => initialIngest,
      GetSettings: async () => initialSettings,
      SetOnlineLookups: async () => {
        throw new Error('settings write failed')
      },
      RefreshGEC: () =>
        new Promise<number>((resolve) => {
          resolveGecSync = resolve
        }),
    })

    const mounted = mountTestRoot(
      createElement(SetupWizardModal, {
        settings: initialSettings,
        ingestStatus: initialIngest,
        onComplete: () => {},
      }),
    )

    const headerBar = mounted.container.querySelector('[role="dialog"] > div') as HTMLElement
    expect(headerBar.querySelector('button')).toBeNull()

    await act(async () => {
      mounted.findButtonByText('NEXT').click()
      await Promise.resolve()
    })
    await act(async () => {
      mounted.findButtonByText('NEXT').click()
      await Promise.resolve()
    })
    act(() => mounted.findButtonByText('NEXT').click())
    expect(mounted.container.textContent).toContain('Step 4 of 4')

    const checkboxes = () =>
      Array.from(mounted.container.querySelectorAll('input[type="checkbox"]')) as HTMLInputElement[]
    await act(async () => {
      checkboxes()[1].click()
      await Promise.resolve()
    })
    expect(spies.SetOnlineLookups).toHaveBeenCalledWith(false)
    expect(checkboxes()[1].checked).toBe(true)
    expect(mounted.container.textContent).toContain('settings write failed')

    act(() => mounted.findButtonByText('SYNC CATALOG NOW').click())
    expect(mounted.findButtonByText('SYNCING...').disabled).toBe(true)
    expect(mounted.findButtonByText('BACK').disabled).toBe(true)
    expect(checkboxes()[0].disabled).toBe(true)
    expect(checkboxes()[1].disabled).toBe(true)

    act(() => {
      mounted.findButtonByText('BACK').click()
    })
    expect(mounted.container.textContent).toContain('Step 4 of 4')

    mounted.unmount()
    await act(async () => {
      resolveGecSync(1200)
      await Promise.resolve()
    })
  })
})
