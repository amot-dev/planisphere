// @vitest-environment happy-dom
import { act, createElement, type ReactElement } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { app, replay } from '../../api/models'

const mockListCommanderLoadGamesOnDate = vi.fn()
const mockMergeCommanders = vi.fn()
const mockResetCommanderOverrides = vi.fn()
const mockSetCareerBaselineScope = vi.fn()
const mockSetCommanderEnabled = vi.fn()
const mockSplitCommander = vi.fn()
const mockUnmergeCommanderAlias = vi.fn()

vi.mock('../../api/backend', () => ({
  ListCommanderLoadGamesOnDate: (...args: unknown[]) => mockListCommanderLoadGamesOnDate(...args),
  MergeCommanders: (...args: unknown[]) => mockMergeCommanders(...args),
  ResetCommanderOverrides: (...args: unknown[]) => mockResetCommanderOverrides(...args),
  SetCareerBaselineScope: (...args: unknown[]) => mockSetCareerBaselineScope(...args),
  SetCommanderEnabled: (...args: unknown[]) => mockSetCommanderEnabled(...args),
  SplitCommander: (...args: unknown[]) => mockSplitCommander(...args),
  UnmergeCommanderAlias: (...args: unknown[]) => mockUnmergeCommanderAlias(...args),
}))

import { CommandersOnlineStep } from '../setup/CommandersOnlineStep'
import { CommandersSection } from './CommandersSection'

;(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

function mountView(element: ReactElement): { container: HTMLDivElement; unmount: () => void } {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  act(() => {
    root.render(element)
  })
  return {
    container,
    unmount: () => {
      act(() => root.unmount())
      container.remove()
    },
  }
}

function findButton(container: HTMLElement, textMatch: string, index = 0): HTMLButtonElement {
  const matches = Array.from(container.querySelectorAll('button')).filter((button) =>
    button.textContent?.includes(textMatch),
  )
  if (!matches[index]) {
    throw new Error(`Expected button "${textMatch}" at index ${index}`)
  }
  return matches[index]
}

function setFormValue(
  element: HTMLInputElement | HTMLSelectElement,
  nextValue: string,
  eventName: 'input' | 'change' = 'change',
): void {
  const prototype =
    element instanceof HTMLSelectElement ? HTMLSelectElement.prototype : HTMLInputElement.prototype
  const descriptor = Object.getOwnPropertyDescriptor(prototype, 'value')
  descriptor?.set?.call(element, nextValue)
  element.dispatchEvent(new Event(eventName, { bubbles: true }))
}

const multiCommanderSettings = () =>
  app.Settings.createFrom({
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
  afterEach(() => {
    vi.clearAllMocks()
  })

  it('hides Career and Baseline Scope for a single commander and renders badges, historic aliases, and scope toggle for multiple profiles', async () => {
    const singleSettings = app.Settings.createFrom({
      commanders: [{ id: 1, profileKey: 'live:f100:era:0', name: 'Solo', enabled: true, eventCount: 90 }],
    })
    const singleView = mountView(
      createElement(CommandersSection, { settings: singleSettings, onSetError: () => {} }),
    )
    try {
      expect(singleView.container.textContent).not.toContain('Career and Baseline Scope')
      expect(singleView.container.textContent).toContain('CMDR Solo')
    } finally {
      singleView.unmount()
    }

    const baseSettings = multiCommanderSettings()
    const updatedScopeSettings = app.Settings.createFrom({
      ...baseSettings,
      careerBaselineScope: 'all_enabled',
    })
    mockSetCareerBaselineScope.mockResolvedValueOnce(updatedScopeSettings)
    mockSetCommanderEnabled.mockResolvedValueOnce(baseSettings)

    const multiView = mountView(
      createElement(CommandersSection, { settings: baseSettings, onSetError: () => {} }),
    )
    try {
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
        findButton(multiView.container, 'All Enabled').click()
        await Promise.resolve()
      })
      expect(mockSetCareerBaselineScope).toHaveBeenCalledWith('all_enabled')
      expect(findButton(multiView.container, 'All Enabled').getAttribute('aria-pressed')).toBe('true')

      const checkboxes = multiView.container.querySelectorAll('input[type="checkbox"]')
      await act(async () => {
        ;(checkboxes[0] as HTMLInputElement).click()
        await Promise.resolve()
      })
      expect(mockSetCommanderEnabled).toHaveBeenCalledWith('live:f100:era:1', false)
    } finally {
      multiView.unmount()
    }
  })

  it('handles Separate alias, Split date with 0, 1, and multiple LoadGame sessions, Reset to Auto, and Merge closing modal', async () => {
    const initial = multiCommanderSettings()
    const collapsedAfterReset = app.Settings.createFrom({
      ...initial,
      commanders: initial.commanders.filter((item) => item.profileKey !== 'live:f100:era:0'),
    })
    mockUnmergeCommanderAlias.mockResolvedValueOnce(initial)
    mockSplitCommander.mockResolvedValue(initial)
    mockResetCommanderOverrides.mockResolvedValueOnce(collapsedAfterReset)
    mockMergeCommanders.mockResolvedValueOnce(initial)

    mockListCommanderLoadGamesOnDate
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([
        replay.CommanderSession.createFrom({
          timestamp: '2026-04-01T08:15:00Z',
          galactic: '3312-04-01 08:15 UTC',
          ship: 'Sidewinder',
          systemName: 'Chamberlain\'s Rest',
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

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    act(() => {
      root.render(createElement(CommandersSection, { settings: initial, onSetError: () => {} }))
    })
    const view = {
      container,
      unmount: () => {
        act(() => root.unmount())
        container.remove()
      },
    }
    try {
      act(() => findButton(view.container, 'MANAGE', 0).click())
      expect(view.container.textContent).toContain('Manage CMDR Vanguard')
      expect(findButton(view.container, 'RESET TO AUTO-DETECTED').disabled).toBe(true)
      // Live profile with no badges renders zero empty badge wrapper rows inside ModalShell.
      const modalBody = view.container.querySelector('.p-5.overflow-y-auto') as HTMLElement
      expect(modalBody.querySelector('.flex.items-center.gap-1\\.5')).toBeNull()

      await act(async () => {
        findButton(view.container, 'SEPARATE').click()
        await Promise.resolve()
      })
      expect(mockUnmergeCommanderAlias).toHaveBeenCalledWith('Rookie', 'live:f100:era:1')

      const dateInput = view.container.querySelector(
        'input[aria-label="Save wipe date"]',
      ) as HTMLInputElement

      // Zero LoadGame sessions on date: dropdown hidden, splits at selectedDate.
      await act(async () => {
        setFormValue(dateInput, '2026-03-31', 'input')
        dateInput.dispatchEvent(new Event('change', { bubbles: true }))
        await Promise.resolve()
      })
      expect(view.container.querySelector('select[aria-label="LoadGame session on selected date"]')).toBeNull()
      await act(async () => {
        findButton(view.container, 'SPLIT AT DATE').click()
        await Promise.resolve()
      })
      expect(mockSplitCommander).toHaveBeenCalledWith('live:f100:era:1', '2026-03-31')

      // Single LoadGame session on date: dropdown hidden, splits at sessions[0].timestamp.
      await act(async () => {
        setFormValue(dateInput, '2026-04-01', 'input')
        dateInput.dispatchEvent(new Event('change', { bubbles: true }))
        await Promise.resolve()
      })
      expect(view.container.querySelector('select[aria-label="LoadGame session on selected date"]')).toBeNull()
      await act(async () => {
        findButton(view.container, 'SPLIT AT DATE').click()
        await Promise.resolve()
      })
      expect(mockSplitCommander).toHaveBeenCalledWith('live:f100:era:1', '2026-04-01T08:15:00Z')

      // Multiple LoadGame sessions on date: dropdown rendered, splits at chosen session timestamp.
      await act(async () => {
        setFormValue(dateInput, '2026-04-02', 'input')
        dateInput.dispatchEvent(new Event('change', { bubbles: true }))
        await Promise.resolve()
      })
      const sessionSelect = view.container.querySelector(
        'select[aria-label="LoadGame session on selected date"]',
      ) as HTMLSelectElement
      expect(sessionSelect).not.toBeNull()
      expect(sessionSelect.textContent).toContain('3312-04-02 19:45 UTC - Sidewinder (HIP 97950)')
      act(() => setFormValue(sessionSelect, '2026-04-02T19:45:00Z'))
      await act(async () => {
        findButton(view.container, 'SPLIT AT DATE').click()
        await Promise.resolve()
      })
      expect(mockSplitCommander).toHaveBeenCalledWith('live:f100:era:1', '2026-04-02T19:45:00Z')
      expect(dateInput.value).toBe('')
      expect(view.container.querySelector('select[aria-label="LoadGame session on selected date"]')).toBeNull()

      // Merge closes the modal on success.
      const mergeSelect = view.container.querySelector(
        'select[aria-label="Target save-slot profile"]',
      ) as HTMLSelectElement
      act(() => setFormValue(mergeSelect, 'beta:f200:era:0'))
      await act(async () => {
        findButton(view.container, 'MERGE INTO SELECTED').click()
        await Promise.resolve()
      })
      expect(mockMergeCommanders).toHaveBeenCalledWith('live:f100:era:1', 'beta:f200:era:0')
      expect(view.container.textContent).not.toContain('Manage CMDR Vanguard')

      // Open profile #2 (hasManualOverride = true) and verify Reset to Auto-Detected clears managingKey when profile collapses.
      act(() => findButton(view.container, 'MANAGE', 1).click())
      expect(view.container.textContent).toContain('Manage CMDR Rookie')
      const resetButton = findButton(view.container, 'RESET TO AUTO-DETECTED')
      expect(resetButton.disabled).toBe(false)
      await act(async () => {
        resetButton.click()
        await Promise.resolve()
      })
      expect(mockResetCommanderOverrides).toHaveBeenCalledWith('live:f100:era:0')
      expect(view.container.textContent).not.toContain('Manage CMDR Rookie')

      // Re-rendering with live:f100:era:0 restored on a later rescan must not reopen the modal.
      act(() => {
        root.render(createElement(CommandersSection, { settings: initial, onSetError: () => {} }))
      })
      expect(view.container.textContent).not.toContain('Manage CMDR Rookie')
    } finally {
      view.unmount()
    }
  })

  it('passes profileKey and renders badges and alias counts in CommandersOnlineStep', () => {
    const onToggleCommander = vi.fn()
    const view = mountView(
      createElement(CommandersOnlineStep, {
        localSettings: multiCommanderSettings(),
        onlineLookups: true,
        gecBusy: false,
        gecSyncedCount: 100,
        isBusy: false,
        onToggleCommander,
        onToggleOnline: () => {},
        onSyncGecNow: () => {},
      }),
    )
    try {
      expect(view.container.textContent).toContain('LEGACY 3.8')
      expect(view.container.textContent).toContain('1,100 events')
      const firstCheckbox = view.container.querySelector('input[type="checkbox"]') as HTMLInputElement
      act(() => firstCheckbox.click())
      expect(onToggleCommander).toHaveBeenCalledWith('live:f100:era:1', false)
    } finally {
      view.unmount()
    }
  })
})
