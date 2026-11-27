// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { app, expedition } from '../../../api/models'
import { makeCommander } from '../../../test/fixtures'
import { mountTestRoot, setFormControlValue } from '../../../test/render'
import { installWailsAppMock } from '../../../test/wailsMock'
import { ExpeditionManageModal } from './ExpeditionManageModal'
import { WaypointImportSection } from './WaypointImportSection'

describe('WaypointImportSection', () => {
  it('omits format badges, preserves duplicate summary, and suppresses invalid warning while parsing', () => {
    const spanshView = mountTestRoot(
      createElement(WaypointImportSection, {
        rawImportText: '"System Name"\n"Sol"\n"Sol"\n"Colonia"',
        parsedImport: {
          format: 'spansh-csv',
          systems: ['Sol', 'Colonia'],
          collapsedDuplicateCount: 1,
          skippedLineCount: 0,
        },
        loadedFileNote: 'Loaded from route.csv',
        loadingFile: false,
        disabled: false,
        onChangeText: () => {},
        onLoadFile: () => {},
        onClear: () => {},
      }),
    )

    expect(spanshView.container.textContent).toContain('2 WAYPOINTS READY')
    expect(spanshView.container.textContent).toContain('1 consecutive duplicate collapsed')
    expect(spanshView.container.textContent).toContain('Loaded from route.csv')
    expect(spanshView.container.textContent).toContain('#01 Sol -> #02 Colonia')
    expect(spanshView.container.textContent).not.toContain('SPANSH CSV')
    expect(spanshView.container.textContent).not.toContain('PLAIN LIST')
    spanshView.unmount()

    const parsingView = mountTestRoot(
      createElement(WaypointImportSection, {
        rawImportText: 'Sol',
        parsedImport: {
          format: 'empty',
          systems: [],
          collapsedDuplicateCount: 0,
          skippedLineCount: 0,
        },
        loadedFileNote: '',
        loadingFile: false,
        parsing: true,
        disabled: false,
        onChangeText: () => {},
        onLoadFile: () => {},
        onClear: () => {},
      }),
    )

    expect(parsingView.container.textContent).not.toContain('No valid star systems detected')
  })
})

describe('ExpeditionManageModal', () => {
  it('disables expedition creation while a waypoint import file is loading and hydrates directly from Go response', async () => {
    let resolveImportFile!: (file: app.WaypointImportFile) => void
    const { spies } = installWailsAppMock({
      BrowseWaypointImportFile: () =>
        new Promise<app.WaypointImportFile>((resolve) => {
          resolveImportFile = resolve
        }),
      CreateExpedition: async () =>
        expedition.Expedition.createFrom({ id: 1, name: 'Unused', status: 'planning' }),
      CreateExpeditionFromImport: async () =>
        expedition.Expedition.createFrom({
          id: 42,
          name: 'spansh-route',
          description: '',
          status: 'planning',
        }),
      ParseWaypointImport: async () =>
        expedition.ParsedWaypointImport.createFrom({ format: 'empty', systems: [] }),
    })

    const onSelect = vi.fn()
    const onClose = vi.fn()
    const existingExpeditions = [
      expedition.Expedition.createFrom({
        id: 1,
        name: 'Bubble Loop',
        description: '',
        status: 'active',
      }),
    ]

    const modalView = mountTestRoot(
      createElement(ExpeditionManageModal, {
        expeditions: existingExpeditions,
        activeId: 1,
        onSelect,
        onClose,
      }),
    )

    const nameInput = modalView.container.querySelector('input[type="text"]') as HTMLInputElement
    expect(nameInput).toBeTruthy()
    expect(nameInput.value).toBe('')

    act(() => {
      modalView.findButtonByText('+ IMPORT WAYPOINTS').click()
    })

    act(() => {
      modalView.findButtonByText('LOAD TXT / CSV').click()
    })

    expect(spies.BrowseWaypointImportFile).toHaveBeenCalledTimes(1)
    expect(modalView.findButtonByText('LOADING...').disabled).toBe(true)

    const createButtonWhileLoading = modalView.findButtonByText('CREATE EXPEDITION')
    expect(createButtonWhileLoading.disabled).toBe(true)
    expect(createButtonWhileLoading.className).toContain('opacity-40')
    expect(createButtonWhileLoading.className).toContain('cursor-not-allowed')

    act(() => {
      createButtonWhileLoading.click()
    })
    expect(spies.CreateExpedition).not.toHaveBeenCalled()
    expect(spies.CreateExpeditionFromImport).not.toHaveBeenCalled()

    await act(async () => {
      resolveImportFile(
        app.WaypointImportFile.createFrom({
          fileName: 'spansh-route.csv',
          suggestedExpeditionName: 'spansh-route',
          normalizedText: 'Sol\nColonia',
          parsed: {
            format: 'spansh-csv',
            systems: ['Sol', 'Colonia'],
            collapsedDuplicateCount: 1,
            skippedLineCount: 0,
          },
        }),
      )
      await Promise.resolve()
    })

    expect(spies.ParseWaypointImport).not.toHaveBeenCalled()
    expect(nameInput.value).toBe('spansh-route')
    expect(modalView.container.textContent).toContain('2 WAYPOINTS READY')
    expect(modalView.container.textContent).toContain(
      'Loaded from spansh-route.csv (1 duplicate body row collapsed)',
    )

    const createAndImportButton = modalView.findButtonByText('CREATE AND IMPORT (2)')
    expect(createAndImportButton.disabled).toBe(false)
    expect(createAndImportButton.className).not.toContain('cursor-not-allowed')

    await act(async () => {
      createAndImportButton.click()
      await Promise.resolve()
    })

    expect(spies.CreateExpeditionFromImport).toHaveBeenCalledWith(
      'spansh-route',
      '',
      'Sol\nColonia',
    )
    expect(onSelect).toHaveBeenCalledWith(42, { persist: false })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('parses immediately on paste, debounces keystrokes by 80 ms, resets blank edits without IPC, and submits raw text via CreateExpeditionFromImport', async () => {
    vi.useFakeTimers()
    const { spies } = installWailsAppMock({
      ParseWaypointImport: async (rawText: string) => {
        const lines = rawText
          .split('\n')
          .map((line) => line.trim())
          .filter((line) => line.length > 0 && !line.startsWith('#'))
        return expedition.ParsedWaypointImport.createFrom({
          format: 'plain',
          systems: lines,
          collapsedDuplicateCount: 0,
          skippedLineCount: 0,
        })
      },
      CreateExpeditionFromImport: async () =>
        expedition.Expedition.createFrom({
          id: 77,
          name: 'Fast Paste Route',
          description: 'Live import',
          status: 'planning',
        }),
    })

    const onSelect = vi.fn()
    const onClose = vi.fn()
    const modalView = mountTestRoot(
      createElement(ExpeditionManageModal, {
        expeditions: [
          expedition.Expedition.createFrom({ id: 1, name: 'Alpha', description: '', status: 'active' }),
        ],
        activeId: 1,
        onSelect,
        onClose,
      }),
    )

    const textInputs = Array.from(
      modalView.container.querySelectorAll('input[type="text"]'),
    ) as HTMLInputElement[]
    act(() => {
      setFormControlValue(textInputs[0], 'Fast Paste Route')
      setFormControlValue(textInputs[1], 'Live import')
      modalView.findButtonByText('+ IMPORT WAYPOINTS').click()
    })

    const textarea = modalView.container.querySelector('textarea') as HTMLTextAreaElement
    expect(textarea).toBeTruthy()

    // Paste triggers immediate 0 ms ParseWaypointImport without advancing timers.
    await act(async () => {
      setFormControlValue(textarea, 'Sol\nColonia\nBeagle Point', { paste: true })
      await Promise.resolve()
    })
    expect(spies.ParseWaypointImport).toHaveBeenCalledTimes(1)
    expect(spies.ParseWaypointImport).toHaveBeenLastCalledWith('Sol\nColonia\nBeagle Point')
    expect(modalView.container.textContent).toContain('3 WAYPOINTS READY')

    // Clearing resets synchronously with zero additional ParseWaypointImport calls.
    act(() => {
      modalView.findButtonByText('CLEAR').click()
    })
    expect(spies.ParseWaypointImport).toHaveBeenCalledTimes(1)
    expect(textarea.value).toBe('')

    // Typing debounces by 80 ms and coalesces rapid keystrokes.
    act(() => {
      setFormControlValue(textarea, 'Shinrarta')
      vi.advanceTimersByTime(40)
      setFormControlValue(textarea, 'Shinrarta Dezhra\nSagittarius A*')
    })
    expect(spies.ParseWaypointImport).toHaveBeenCalledTimes(1)
    expect(modalView.container.textContent).not.toContain('No valid star systems detected')

    // Submitting before the 80 ms preview debounce fires still sends the latest rawImportText.
    await act(async () => {
      modalView.findButtonByText('CREATE EXPEDITION').click()
      await Promise.resolve()
    })
    expect(spies.CreateExpeditionFromImport).toHaveBeenCalledWith(
      'Fast Paste Route',
      'Live import',
      'Shinrarta Dezhra\nSagittarius A*',
    )
    expect(spies.ParseWaypointImport).toHaveBeenCalledTimes(1)
    expect(onSelect).toHaveBeenCalledWith(77, { persist: false })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('debounces typing after 80 ms and ignores stale out-of-order ParseWaypointImport responses', async () => {
    vi.useFakeTimers()
    let resolveFirstParse!: (parsed: expedition.ParsedWaypointImport) => void
    let callNumber = 0
    const { spies } = installWailsAppMock({
      ParseWaypointImport: (rawText: string) => {
        callNumber += 1
        if (callNumber === 1) {
          return new Promise<expedition.ParsedWaypointImport>((resolve) => {
            resolveFirstParse = resolve
          })
        }
        return Promise.resolve(
          expedition.ParsedWaypointImport.createFrom({
            format: 'plain',
            systems: rawText.split('\n').filter(Boolean),
            collapsedDuplicateCount: 0,
            skippedLineCount: 0,
          }),
        )
      },
    })

    const modalView = mountTestRoot(
      createElement(ExpeditionManageModal, {
        expeditions: [
          expedition.Expedition.createFrom({ id: 1, name: 'Alpha', description: '', status: 'active' }),
        ],
        activeId: 1,
        onSelect: () => {},
        onClose: () => {},
      }),
    )

    act(() => {
      modalView.findButtonByText('+ IMPORT WAYPOINTS').click()
    })
    const textarea = modalView.container.querySelector('textarea') as HTMLTextAreaElement

    await act(async () => {
      setFormControlValue(textarea, 'Sol')
      vi.advanceTimersByTime(80)
      await Promise.resolve()
    })
    expect(spies.ParseWaypointImport).toHaveBeenCalledTimes(1)

    // Second paste arrives and resolves before the first in-flight RPC settles.
    await act(async () => {
      setFormControlValue(textarea, 'Colonia\nRohini', { paste: true })
      await Promise.resolve()
    })
    expect(spies.ParseWaypointImport).toHaveBeenCalledTimes(2)
    expect(modalView.container.textContent).toContain('2 WAYPOINTS READY')

    await act(async () => {
      resolveFirstParse(
        expedition.ParsedWaypointImport.createFrom({
          format: 'plain',
          systems: ['Sol'],
          collapsedDuplicateCount: 0,
          skippedLineCount: 0,
        }),
      )
      await Promise.resolve()
    })
    expect(modalView.container.textContent).toContain('2 WAYPOINTS READY')
    expect(modalView.container.textContent).toContain('#01 Colonia -> #02 Rohini')
  })

  it('locks action and close controls while cloning and selects the clone with persist: false', async () => {
    let resolveClone!: (cloned: expedition.Expedition) => void
    const { spies } = installWailsAppMock({
      CloneExpedition: () =>
        new Promise<expedition.Expedition>((resolve) => {
          resolveClone = resolve
        }),
    })

    const onSelect = vi.fn()
    const onClose = vi.fn()
    const savedExpeditions = [
      expedition.Expedition.createFrom({ id: 1, name: 'Alpha', description: '', status: 'active' }),
      expedition.Expedition.createFrom({ id: 2, name: 'Beta', description: '', status: 'planning' }),
    ]

    const modalView = mountTestRoot(
      createElement(ExpeditionManageModal, {
        expeditions: savedExpeditions,
        activeId: 1,
        onSelect,
        onClose,
      }),
    )

    act(() => {
      modalView.findButtonByText('CLONE').click()
    })

    expect(spies.CloneExpedition).toHaveBeenCalledWith(1)
    expect(modalView.findButtonByText('CLONE').disabled).toBe(true)
    expect(modalView.findButtonByText('RENAME').disabled).toBe(true)
    expect(modalView.findButtonByText('DELETE').disabled).toBe(true)
    expect(modalView.findButtonByText('Alpha').disabled).toBe(true)
    const closeHeaderButton = modalView.container.querySelector('button') as HTMLButtonElement
    expect(closeHeaderButton.disabled).toBe(true)

    await act(async () => {
      resolveClone(
        expedition.Expedition.createFrom({
          id: 99,
          name: 'Alpha (Copy)',
          description: '',
          status: 'planning',
        }),
      )
      await Promise.resolve()
    })

    expect(onSelect).toHaveBeenCalledWith(99, { persist: false })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('renders EDIT with Save-Slot Scope picker when multiple commanders exist and calls UpdateExpeditionScoped', async () => {
    const { spies } = installWailsAppMock({
      UpdateExpedition: async () => {},
      UpdateExpeditionScoped: async () => {},
    })
    const savedExpeditions = [
      expedition.Expedition.createFrom({
        id: 5,
        name: 'Outer Arm Survey',
        description: 'Initial',
        status: 'active',
        commanderScope: 'all_enabled',
        commanderProfileIds: [],
      }),
    ]
    const commanders = [
      makeCommander({ id: 10, profileKey: 'live:f10:era:0', name: 'Primary', enabled: true, eventCount: 300 }),
      makeCommander({ id: 20, profileKey: 'live:f20:era:0', name: 'Secondary', enabled: false, eventCount: 80 }),
    ]

    const modalView = mountTestRoot(
      createElement(ExpeditionManageModal, {
        expeditions: savedExpeditions,
        activeId: 5,
        commanders,
        onSelect: () => {},
        onClose: () => {},
      }),
    )

    act(() => modalView.findButtonByText('EDIT').click())
    expect(modalView.container.textContent).toContain('Save-Slot Scope')

    act(() => modalView.findButtonByText('Selected Save Slot(s)').click())
    const checkboxes = Array.from(
      modalView.container.querySelectorAll('input[type="checkbox"]'),
    ) as HTMLInputElement[]
    expect(checkboxes).toHaveLength(2)
    expect(checkboxes[0].checked).toBe(true)
    expect(checkboxes[1].checked).toBe(false)

    await act(async () => {
      modalView.findButtonByText('SAVE').click()
      await Promise.resolve()
    })
    expect(spies.UpdateExpeditionScoped).toHaveBeenCalledWith(
      5,
      'Outer Arm Survey',
      'Initial',
      'selected',
      [10],
    )
    expect(spies.UpdateExpedition).not.toHaveBeenCalled()
  })
})
