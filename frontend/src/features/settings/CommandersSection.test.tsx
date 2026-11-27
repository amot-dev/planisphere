// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { describe, expect, it } from 'vitest'
import { app, replay } from '../../api/models'
import { makeSettings } from '../../test/fixtures'
import { mountTestRoot, setFormControlValue } from '../../test/render'
import { installWailsAppMock } from '../../test/wailsMock'
import { CommandersSection } from './CommandersSection'

const multiCommanderSettings = () =>
  makeSettings({
    careerBaselineScope: 'expedition',
    commanders: [
      {
        id: 1,
        profileKey: 'live:f100:era:1',
        name: 'Vanguard',
        fid: 'F100',
        universe: 'live',
        eraIndex: 1,
        isArchivedCareer: false,
        hasManualOverride: false,
        enabled: true,
        firstGalactic: '3310-02-01 12:00 UTC',
        lastGalactic: '3312-11-20 18:30 UTC',
        eventCount: 1500,
        aliases: [
          { name: 'Vanguard', fid: 'F100', universe: 'live', eventCount: 1100, isCurrent: true, source: 'auto' },
          { name: 'Rookie', fid: 'F100', universe: 'live', eventCount: 400, isCurrent: false, source: 'auto' },
        ],
      },
      {
        id: 2,
        profileKey: 'live:f100:era:0',
        name: 'Rookie',
        fid: 'F100',
        universe: 'legacy',
        eraIndex: 0,
        isArchivedCareer: true,
        hasManualOverride: true,
        enabled: false,
        firstGalactic: '3308-01-01 00:00 UTC',
        lastGalactic: '3308-11-28 00:00 UTC',
        eventCount: 320,
        aliases: [],
      },
      {
        id: 3,
        profileKey: 'beta:f200:era:0',
        name: 'Tester',
        fid: 'F200',
        universe: 'beta',
        eraIndex: 0,
        isArchivedCareer: false,
        hasManualOverride: false,
        enabled: true,
        eventCount: 55,
        aliases: [],
      },
    ],
  })

describe('CommandersSection and CommanderManageModal', () => {
  it('hides Career and Baseline Scope for a single commander and renders badges, historic aliases, and scope toggle for multiple profiles', async () => {
    const singleSettings = makeSettings({
      commanders: [{ id: 1, profileKey: 'live:f100:era:0', name: 'Solo', enabled: true, eventCount: 90 }],
    })
    const singleView = mountTestRoot(
      createElement(CommandersSection, { settings: singleSettings, onSetError: () => {} }),
    )
    expect(singleView.container.textContent).not.toContain('Career and Baseline Scope')
    expect(singleView.container.textContent).toContain('CMDR Solo')
    singleView.unmount()

    const baseSettings = multiCommanderSettings()
    const updatedScopeSettings = app.Settings.createFrom({
      ...baseSettings,
      careerBaselineScope: 'all_enabled',
    })
    const { spies } = installWailsAppMock({
      SetCareerBaselineScope: async () => updatedScopeSettings,
      SetCommanderEnabled: async () => baseSettings,
    })

    const multiView = mountTestRoot(
      createElement(CommandersSection, { settings: baseSettings, onSetError: () => {} }),
    )
    const text = multiView.container.textContent ?? ''
    expect(text).toContain('Career and Baseline Scope')
    expect(text).toContain('LEGACY 3.8')
    expect(text).toContain('PRE-WIPE')
    expect(text).toContain('MANUAL')
    expect(text).toContain('BETA')
    expect(text).toContain('FID F100 · 3310-02-01 - 3312-11-20')
    expect(text).toContain('(CURRENT)')
    expect(text).toContain('1,100 events')
    expect(text).toContain('(HISTORIC)')
    expect(text).toContain('400 events')

    await act(async () => {
      multiView.findButtonByText('All Enabled').click()
      await Promise.resolve()
    })
    expect(spies.SetCareerBaselineScope).toHaveBeenCalledWith('all_enabled')
    expect(multiView.findButtonByText('All Enabled').getAttribute('aria-pressed')).toBe('true')

    const checkboxes = multiView.container.querySelectorAll('input[type="checkbox"]')
    await act(async () => {
      ;(checkboxes[0] as HTMLInputElement).click()
      await Promise.resolve()
    })
    expect(spies.SetCommanderEnabled).toHaveBeenCalledWith('live:f100:era:1', false)
  })

  it('handles Separate alias, Split date with 0, 1, and multiple LoadGame sessions, Reset to Auto, and Merge closing modal', async () => {
    const initial = multiCommanderSettings()
    const collapsedAfterReset = app.Settings.createFrom({
      ...initial,
      commanders: initial.commanders.filter((item) => item.profileKey !== 'live:f100:era:0'),
    })
    const { spies } = installWailsAppMock({
      UnmergeCommanderAlias: async () => initial,
      SplitCommander: async () => initial,
      ResetCommanderOverrides: async () => collapsedAfterReset,
      MergeCommanders: async () => initial,
      ListCommanderLoadGamesOnDate: async () => [],
    })

    spies.ListCommanderLoadGamesOnDate
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([
        replay.CommanderSession.createFrom({
          timestamp: '2026-04-01T08:15:00Z',
          galactic: '3312-04-01 08:15 UTC',
          ship: 'Sidewinder',
          systemName: "Chamberlain's Rest",
        }),
      ])
      .mockResolvedValueOnce([
        replay.CommanderSession.createFrom({
          timestamp: '2026-04-02T09:00:00Z',
          galactic: '3312-04-02 09:00 UTC',
          ship: 'Mandalay',
          systemName: 'Shinrarta Dezhra',
        }),
        replay.CommanderSession.createFrom({
          timestamp: '2026-04-02T19:45:00Z',
          galactic: '3312-04-02 19:45 UTC',
          ship: 'Sidewinder',
          systemName: 'HIP 97950',
        }),
      ])

    const view = mountTestRoot(
      createElement(CommandersSection, { settings: initial, onSetError: () => {} }),
    )
    act(() => view.findButtonByText('MANAGE', { index: 0 }).click())
    expect(view.container.textContent).toContain('Manage CMDR Vanguard')
    expect(view.findButtonByText('RESET TO AUTO-DETECTED').disabled).toBe(true)
    // Live profile with no badges renders zero empty badge wrapper rows inside ModalShell.
    const modalBody = view.container.querySelector('.p-5.overflow-y-auto') as HTMLElement
    expect(modalBody.querySelector('.flex.items-center.gap-1\\.5')).toBeNull()

    await act(async () => {
      view.findButtonByText('SEPARATE').click()
      await Promise.resolve()
    })
    expect(spies.UnmergeCommanderAlias).toHaveBeenCalledWith('Rookie', 'live:f100:era:1')

    const dateInput = view.container.querySelector(
      'input[aria-label="Save wipe date"]',
    ) as HTMLInputElement

    // Zero LoadGame sessions on date: dropdown hidden, splits at selectedDate.
    await act(async () => {
      setFormControlValue(dateInput, '2026-03-31')
      dateInput.dispatchEvent(new Event('change', { bubbles: true }))
      await Promise.resolve()
    })
    expect(view.container.querySelector('select[aria-label="LoadGame session on selected date"]')).toBeNull()
    await act(async () => {
      view.findButtonByText('SPLIT AT DATE').click()
      await Promise.resolve()
    })
    expect(spies.SplitCommander).toHaveBeenCalledWith('live:f100:era:1', '2026-03-31')

    // Single LoadGame session on date: dropdown hidden, splits at sessions[0].timestamp.
    await act(async () => {
      setFormControlValue(dateInput, '2026-04-01')
      dateInput.dispatchEvent(new Event('change', { bubbles: true }))
      await Promise.resolve()
    })
    expect(view.container.querySelector('select[aria-label="LoadGame session on selected date"]')).toBeNull()
    await act(async () => {
      view.findButtonByText('SPLIT AT DATE').click()
      await Promise.resolve()
    })
    expect(spies.SplitCommander).toHaveBeenCalledWith('live:f100:era:1', '2026-04-01T08:15:00Z')

    // Multiple LoadGame sessions on date: dropdown rendered, splits at chosen session timestamp.
    await act(async () => {
      setFormControlValue(dateInput, '2026-04-02')
      dateInput.dispatchEvent(new Event('change', { bubbles: true }))
      await Promise.resolve()
    })
    const sessionSelect = view.container.querySelector(
      'select[aria-label="LoadGame session on selected date"]',
    ) as HTMLSelectElement
    expect(sessionSelect).not.toBeNull()
    expect(sessionSelect.textContent).toContain('3312-04-02 19:45 UTC - Sidewinder (HIP 97950)')
    act(() => setFormControlValue(sessionSelect, '2026-04-02T19:45:00Z'))
    await act(async () => {
      view.findButtonByText('SPLIT AT DATE').click()
      await Promise.resolve()
    })
    expect(spies.SplitCommander).toHaveBeenCalledWith('live:f100:era:1', '2026-04-02T19:45:00Z')
    expect(dateInput.value).toBe('')
    expect(view.container.querySelector('select[aria-label="LoadGame session on selected date"]')).toBeNull()

    // Merge closes the modal on success.
    const mergeSelect = view.container.querySelector(
      'select[aria-label="Target save-slot profile"]',
    ) as HTMLSelectElement
    act(() => setFormControlValue(mergeSelect, 'beta:f200:era:0'))
    await act(async () => {
      view.findButtonByText('MERGE INTO SELECTED').click()
      await Promise.resolve()
    })
    expect(spies.MergeCommanders).toHaveBeenCalledWith('live:f100:era:1', 'beta:f200:era:0')
    expect(view.container.textContent).not.toContain('Manage CMDR Vanguard')

    // Open profile #2 (hasManualOverride = true) and verify Reset to Auto-Detected clears managingKey when profile collapses.
    act(() => view.findButtonByText('MANAGE', { index: 1 }).click())
    expect(view.container.textContent).toContain('Manage CMDR Rookie')
    const resetButton = view.findButtonByText('RESET TO AUTO-DETECTED')
    expect(resetButton.disabled).toBe(false)
    await act(async () => {
      resetButton.click()
      await Promise.resolve()
    })
    expect(spies.ResetCommanderOverrides).toHaveBeenCalledWith('live:f100:era:0')
    expect(view.container.textContent).not.toContain('Manage CMDR Rookie')

    // Re-rendering with live:f100:era:0 restored on a later rescan must not reopen the modal.
    view.rerender(createElement(CommandersSection, { settings: initial, onSetError: () => {} }))
    expect(view.container.textContent).not.toContain('Manage CMDR Rookie')
  })
})
