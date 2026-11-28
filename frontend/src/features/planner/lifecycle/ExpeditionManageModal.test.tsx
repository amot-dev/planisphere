// @vitest-environment happy-dom
import { act } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { app, expedition } from '../../../api/models'
import { makeCommander } from '../../../test/fixtures'
import { mountTestRoot, setFormControlValue } from '../../../test/render'
import { installWailsAppMock } from '../../../test/wailsMock'
import { ExpeditionManageModal } from './ExpeditionManageModal'

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
      <ExpeditionManageModal
        expeditions={existingExpeditions}
        activeId={1}
        onSelect={onSelect}
        onClose={onClose}
      />,
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
      <ExpeditionManageModal
        expeditions={[
          expedition.Expedition.createFrom({ id: 1, name: 'Alpha', description: '', status: 'active' }),
        ]}
        activeId={1}
        onSelect={onSelect}
        onClose={onClose}
      />,
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
      <ExpeditionManageModal
        expeditions={[
          expedition.Expedition.createFrom({ id: 1, name: 'Alpha', description: '', status: 'active' }),
        ]}
        activeId={1}
        onSelect={() => {}}
        onClose={() => {}}
      />,
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
      <ExpeditionManageModal
        expeditions={savedExpeditions}
        activeId={1}
        onSelect={onSelect}
        onClose={onClose}
      />,
    )

    const cloneButton = modalView.container.querySelector(
      'button[aria-label="Clone expedition"]',
    ) as HTMLButtonElement
    const editButton = modalView.container.querySelector(
      'button[aria-label="Edit expedition"]',
    ) as HTMLButtonElement
    const deleteButton = modalView.container.querySelector(
      'button[aria-label="Delete expedition"]',
    ) as HTMLButtonElement
    expect(cloneButton.querySelector('svg')).not.toBeNull()
    expect(editButton.querySelector('svg')).not.toBeNull()
    expect(deleteButton.querySelector('svg')).not.toBeNull()
    expect(editButton.title).toBe('Edit expedition name and description')

    act(() => {
      cloneButton.click()
    })

    expect(spies.CloneExpedition).toHaveBeenCalledWith(1)
    expect(cloneButton.disabled).toBe(true)
    expect(editButton.disabled).toBe(true)
    expect(deleteButton.disabled).toBe(true)
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

  it('supports canceling and saving single-commander inline edits from the edit icon button', async () => {
    const { spies } = installWailsAppMock({
      UpdateExpedition: async () => {},
    })
    const modalView = mountTestRoot(
      <ExpeditionManageModal
        expeditions={[
          expedition.Expedition.createFrom({
            id: 3,
            name: 'Sagittarius Loop',
            description: 'Draft notes',
            status: 'planning',
          }),
        ]}
        activeId={3}
        onSelect={() => {}}
        onClose={() => {}}
      />,
    )

    const editButton = modalView.container.querySelector(
      'button[aria-label="Edit expedition"]',
    ) as HTMLButtonElement
    act(() => editButton.click())
    expect(modalView.findButtonByText('SAVE')).toBeTruthy()
    expect(modalView.findButtonByText('CANCEL')).toBeTruthy()

    act(() => modalView.findButtonByText('CANCEL').click())
    expect(modalView.queryButtonByText('SAVE')).toBeNull()
    expect(spies.UpdateExpedition).not.toHaveBeenCalled()

    const reopenedEditButton = modalView.container.querySelector(
      'button[aria-label="Edit expedition"]',
    ) as HTMLButtonElement
    act(() => reopenedEditButton.click())
    const textInputs = Array.from(
      modalView.container.querySelectorAll('input[type="text"]'),
    ) as HTMLInputElement[]
    act(() => {
      setFormControlValue(textInputs[2], 'Sagittarius Core')
      setFormControlValue(textInputs[3], 'Updated notes')
    })

    await act(async () => {
      modalView.findButtonByText('SAVE').click()
      await Promise.resolve()
    })
    expect(spies.UpdateExpedition).toHaveBeenCalledWith(3, 'Sagittarius Core', 'Updated notes')
  })

  it('renders edit icon button with single-commander save-slot selector when multiple commanders exist and calls UpdateExpeditionScoped', async () => {
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
        commanderProfileId: 0,
      }),
    ]
    const commanders = [
      makeCommander({
        id: 10,
        profileKey: 'live:f10:era:0',
        name: 'Primary',
        enabled: true,
        lastSeen: '2026-02-01T00:00:00Z',
        eventCount: 300,
      }),
      makeCommander({
        id: 20,
        profileKey: 'live:f20:era:0',
        name: 'Secondary',
        enabled: false,
        lastSeen: '2026-03-01T00:00:00Z',
        eventCount: 80,
      }),
    ]

    const modalView = mountTestRoot(
      <ExpeditionManageModal
        expeditions={savedExpeditions}
        activeId={5}
        commanders={commanders}
        onSelect={() => {}}
        onClose={() => {}}
      />,
    )

    const editButton = modalView.container.querySelector(
      'button[aria-label="Edit expedition"]',
    ) as HTMLButtonElement
    expect(editButton.title).toBe('Edit expedition name, description, and commander save slot')
    act(() => editButton.click())
    expect(modalView.container.textContent).toContain('Commander Save Slot')

    const radios = Array.from(
      modalView.container.querySelectorAll('input[type="radio"]'),
    ) as HTMLInputElement[]
    expect(radios).toHaveLength(2)
    expect(radios[0].checked).toBe(true)
    expect(radios[1].checked).toBe(false)

    act(() => radios[1].click())
    expect(radios[0].checked).toBe(false)
    expect(radios[1].checked).toBe(true)

    await act(async () => {
      modalView.findButtonByText('SAVE').click()
      await Promise.resolve()
    })
    expect(spies.UpdateExpeditionScoped).toHaveBeenCalledWith(
      5,
      'Outer Arm Survey',
      'Initial',
      20,
    )
    expect(spies.UpdateExpedition).not.toHaveBeenCalled()
  })
})
