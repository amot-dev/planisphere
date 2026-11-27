// @vitest-environment happy-dom
import { act, createElement, type ReactElement } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { app, expedition, replay } from '../../../api/models'

const mockBrowseWaypointImportFile = vi.fn<() => Promise<app.WaypointImportFile>>()
const mockCloneExpedition = vi.fn()
const mockCreateExpedition = vi.fn()
const mockCreateExpeditionFromImport = vi.fn()
const mockParseWaypointImport = vi.fn<
  (rawText: string) => Promise<expedition.ParsedWaypointImport>
>()
const mockUpdateExpedition = vi.fn()
const mockUpdateExpeditionScoped = vi.fn()

vi.mock('../../../api/backend', () => ({
  BrowseWaypointImportFile: () => mockBrowseWaypointImportFile(),
  CloneExpedition: (...args: unknown[]) => mockCloneExpedition(...args),
  CreateExpedition: (...args: unknown[]) => mockCreateExpedition(...args),
  CreateExpeditionFromImport: (...args: unknown[]) => mockCreateExpeditionFromImport(...args),
  DeleteExpedition: vi.fn(),
  ParseWaypointImport: (rawText: string) => mockParseWaypointImport(rawText),
  UpdateExpedition: (...args: unknown[]) => mockUpdateExpedition(...args),
  UpdateExpeditionScoped: (...args: unknown[]) => mockUpdateExpeditionScoped(...args),
}))

import { ExpeditionManageModal } from './ExpeditionManageModal'
import { WaypointImportSection } from './WaypointImportSection'

;(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

function mountElement(element: ReactElement): { container: HTMLDivElement; unmount: () => void } {
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

function findButtonByText(container: HTMLElement, textMatch: string): HTMLButtonElement {
  const buttons = Array.from(container.querySelectorAll('button'))
  const button =
    buttons.find((element) => element.textContent?.trim() === textMatch) ??
    buttons.find((element) => element.textContent?.includes(textMatch))
  if (!button) {
    throw new Error(`Expected button containing "${textMatch}" to exist`)
  }
  return button
}

function setInputValue(input: HTMLInputElement, nextValue: string): void {
  const descriptor = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')
  descriptor?.set?.call(input, nextValue)
  input.dispatchEvent(new Event('input', { bubbles: true }))
}

function setTextareaValue(
  textarea: HTMLTextAreaElement,
  nextValue: string,
  options?: { paste?: boolean },
): void {
  if (options?.paste) {
    textarea.dispatchEvent(new Event('paste', { bubbles: true }))
  }
  const descriptor = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')
  descriptor?.set?.call(textarea, nextValue)
  textarea.dispatchEvent(new Event('input', { bubbles: true }))
}

describe('WaypointImportSection', () => {
  it('omits format badges, preserves duplicate summary, and suppresses invalid warning while parsing', () => {
    const spanshView = mountElement(
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

    try {
      expect(spanshView.container.textContent).toContain('2 WAYPOINTS READY')
      expect(spanshView.container.textContent).toContain('1 consecutive duplicate collapsed')
      expect(spanshView.container.textContent).toContain('Loaded from route.csv')
      expect(spanshView.container.textContent).toContain('#01 Sol -> #02 Colonia')
      expect(spanshView.container.textContent).not.toContain('SPANSH CSV')
      expect(spanshView.container.textContent).not.toContain('PLAIN LIST')
    } finally {
      spanshView.unmount()
    }

    const parsingView = mountElement(
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

    try {
      expect(parsingView.container.textContent).not.toContain('No valid star systems detected')
    } finally {
      parsingView.unmount()
    }
  })
})

describe('ExpeditionManageModal', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.resetAllMocks()
  })

  it('disables expedition creation while a waypoint import file is loading and hydrates directly from Go response', async () => {
    let resolveImportFile!: (file: app.WaypointImportFile) => void
    mockBrowseWaypointImportFile.mockImplementation(
      () =>
        new Promise<app.WaypointImportFile>((resolve) => {
          resolveImportFile = resolve
        }),
    )
    mockCreateExpeditionFromImport.mockResolvedValue(
      expedition.Expedition.createFrom({
        id: 42,
        name: 'spansh-route',
        description: '',
        status: 'planning',
      }),
    )

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

    const modalView = mountElement(
      createElement(ExpeditionManageModal, {
        expeditions: existingExpeditions,
        activeId: 1,
        onSelect,
        onClose,
      }),
    )

    try {
      const nameInput = modalView.container.querySelector('input[type="text"]') as HTMLInputElement
      expect(nameInput).toBeTruthy()
      expect(nameInput.value).toBe('')

      const importToggleButton = findButtonByText(modalView.container, '+ IMPORT WAYPOINTS')
      act(() => {
        importToggleButton.click()
      })

      const loadFileButton = findButtonByText(modalView.container, 'LOAD TXT / CSV')
      act(() => {
        loadFileButton.click()
      })

      expect(mockBrowseWaypointImportFile).toHaveBeenCalledTimes(1)
      expect(findButtonByText(modalView.container, 'LOADING...').disabled).toBe(true)

      const createButtonWhileLoading = findButtonByText(modalView.container, 'CREATE EXPEDITION')
      expect(createButtonWhileLoading.disabled).toBe(true)
      expect(createButtonWhileLoading.className).toContain('opacity-40')
      expect(createButtonWhileLoading.className).toContain('cursor-not-allowed')

      act(() => {
        createButtonWhileLoading.click()
      })
      expect(mockCreateExpedition).not.toHaveBeenCalled()
      expect(mockCreateExpeditionFromImport).not.toHaveBeenCalled()

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

      expect(mockParseWaypointImport).not.toHaveBeenCalled()
      expect(nameInput.value).toBe('spansh-route')
      expect(modalView.container.textContent).toContain('2 WAYPOINTS READY')
      expect(modalView.container.textContent).toContain(
        'Loaded from spansh-route.csv (1 duplicate body row collapsed)',
      )

      const createAndImportButton = findButtonByText(modalView.container, 'CREATE AND IMPORT (2)')
      expect(createAndImportButton.disabled).toBe(false)
      expect(createAndImportButton.className).not.toContain('cursor-not-allowed')

      await act(async () => {
        createAndImportButton.click()
        await Promise.resolve()
      })

      expect(mockCreateExpeditionFromImport).toHaveBeenCalledWith(
        'spansh-route',
        '',
        'Sol\nColonia',
      )
      expect(onSelect).toHaveBeenCalledWith(42, { persist: false })
      expect(onClose).toHaveBeenCalledTimes(1)
    } finally {
      modalView.unmount()
    }
  })

  it('parses immediately on paste, debounces keystrokes by 80 ms, resets blank edits without IPC, and submits raw text via CreateExpeditionFromImport', async () => {
    vi.useFakeTimers()
    mockParseWaypointImport.mockImplementation(async (rawText: string) => {
      const lines = rawText
        .split('\n')
        .map((line) => line.trim())
        .filter((line) => line.length > 0 && !line.startsWith('#'))
      return expedition.ParsedWaypointImport.createFrom({
        format: lines.length > 0 ? 'plain' : 'plain',
        systems: lines,
        collapsedDuplicateCount: 0,
        skippedLineCount: 0,
      })
    })
    mockCreateExpeditionFromImport.mockResolvedValue(
      expedition.Expedition.createFrom({
        id: 77,
        name: 'Fast Paste Route',
        description: 'Live import',
        status: 'planning',
      }),
    )

    const onSelect = vi.fn()
    const onClose = vi.fn()
    const modalView = mountElement(
      createElement(ExpeditionManageModal, {
        expeditions: [
          expedition.Expedition.createFrom({ id: 1, name: 'Alpha', description: '', status: 'active' }),
        ],
        activeId: 1,
        onSelect,
        onClose,
      }),
    )

    try {
      const textInputs = Array.from(
        modalView.container.querySelectorAll('input[type="text"]'),
      ) as HTMLInputElement[]
      act(() => {
        setInputValue(textInputs[0], 'Fast Paste Route')
        setInputValue(textInputs[1], 'Live import')
        findButtonByText(modalView.container, '+ IMPORT WAYPOINTS').click()
      })

      const textarea = modalView.container.querySelector('textarea') as HTMLTextAreaElement
      expect(textarea).toBeTruthy()

      // Paste triggers immediate 0 ms ParseWaypointImport without advancing timers.
      await act(async () => {
        setTextareaValue(textarea, 'Sol\nColonia\nBeagle Point', { paste: true })
        await Promise.resolve()
      })
      expect(mockParseWaypointImport).toHaveBeenCalledTimes(1)
      expect(mockParseWaypointImport).toHaveBeenLastCalledWith('Sol\nColonia\nBeagle Point')
      expect(modalView.container.textContent).toContain('3 WAYPOINTS READY')

      // Clearing resets synchronously with zero additional ParseWaypointImport calls.
      act(() => {
        findButtonByText(modalView.container, 'CLEAR').click()
      })
      expect(mockParseWaypointImport).toHaveBeenCalledTimes(1)
      expect(textarea.value).toBe('')

      // Typing debounces by 80 ms and coalesces rapid keystrokes.
      act(() => {
        setTextareaValue(textarea, 'Shinrarta')
        vi.advanceTimersByTime(40)
        setTextareaValue(textarea, 'Shinrarta Dezhra\nSagittarius A*')
      })
      expect(mockParseWaypointImport).toHaveBeenCalledTimes(1)
      expect(modalView.container.textContent).not.toContain('No valid star systems detected')

      // Submitting before the 80 ms preview debounce fires still sends the latest rawImportText.
      await act(async () => {
        findButtonByText(modalView.container, 'CREATE EXPEDITION').click()
        await Promise.resolve()
      })
      expect(mockCreateExpeditionFromImport).toHaveBeenCalledWith(
        'Fast Paste Route',
        'Live import',
        'Shinrarta Dezhra\nSagittarius A*',
      )
      expect(mockParseWaypointImport).toHaveBeenCalledTimes(1)
      expect(onSelect).toHaveBeenCalledWith(77, { persist: false })
      expect(onClose).toHaveBeenCalledTimes(1)
    } finally {
      modalView.unmount()
    }
  })

  it('debounces typing after 80 ms and ignores stale out-of-order ParseWaypointImport responses', async () => {
    vi.useFakeTimers()
    let resolveFirstParse!: (parsed: expedition.ParsedWaypointImport) => void
    let callNumber = 0
    mockParseWaypointImport.mockImplementation((rawText: string) => {
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
    })

    const modalView = mountElement(
      createElement(ExpeditionManageModal, {
        expeditions: [
          expedition.Expedition.createFrom({ id: 1, name: 'Alpha', description: '', status: 'active' }),
        ],
        activeId: 1,
        onSelect: () => {},
        onClose: () => {},
      }),
    )

    try {
      act(() => {
        findButtonByText(modalView.container, '+ IMPORT WAYPOINTS').click()
      })
      const textarea = modalView.container.querySelector('textarea') as HTMLTextAreaElement

      await act(async () => {
        setTextareaValue(textarea, 'Sol')
        vi.advanceTimersByTime(80)
        await Promise.resolve()
      })
      expect(mockParseWaypointImport).toHaveBeenCalledTimes(1)

      // Second paste arrives and resolves before the first in-flight RPC settles.
      await act(async () => {
        setTextareaValue(textarea, 'Colonia\nRohini', { paste: true })
        await Promise.resolve()
      })
      expect(mockParseWaypointImport).toHaveBeenCalledTimes(2)
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
    } finally {
      modalView.unmount()
    }
  })

  it('locks action and close controls while cloning and selects the clone with persist: false', async () => {
    let resolveClone!: (cloned: expedition.Expedition) => void
    mockCloneExpedition.mockImplementation(
      () =>
        new Promise<expedition.Expedition>((resolve) => {
          resolveClone = resolve
        }),
    )

    const onSelect = vi.fn()
    const onClose = vi.fn()
    const savedExpeditions = [
      expedition.Expedition.createFrom({ id: 1, name: 'Alpha', description: '', status: 'active' }),
      expedition.Expedition.createFrom({ id: 2, name: 'Beta', description: '', status: 'planning' }),
    ]

    const modalView = mountElement(
      createElement(ExpeditionManageModal, {
        expeditions: savedExpeditions,
        activeId: 1,
        onSelect,
        onClose,
      }),
    )

    try {
      const cloneButton = findButtonByText(modalView.container, 'CLONE')
      act(() => {
        cloneButton.click()
      })

      expect(mockCloneExpedition).toHaveBeenCalledWith(1)
      expect(findButtonByText(modalView.container, 'CLONE').disabled).toBe(true)
      expect(findButtonByText(modalView.container, 'RENAME').disabled).toBe(true)
      expect(findButtonByText(modalView.container, 'DELETE').disabled).toBe(true)
      expect(findButtonByText(modalView.container, 'Alpha').disabled).toBe(true)
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
    } finally {
      modalView.unmount()
    }
  })

  it('renders EDIT with Save-Slot Scope picker when multiple commanders exist and calls UpdateExpeditionScoped', async () => {
    mockUpdateExpeditionScoped.mockResolvedValueOnce(undefined)
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
      replay.Commander.createFrom({ id: 10, profileKey: 'live:f10:era:0', name: 'Primary', enabled: true, eventCount: 300 }),
      replay.Commander.createFrom({ id: 20, profileKey: 'live:f20:era:0', name: 'Secondary', enabled: false, eventCount: 80 }),
    ]

    const modalView = mountElement(
      createElement(ExpeditionManageModal, {
        expeditions: savedExpeditions,
        activeId: 5,
        commanders,
        onSelect: () => {},
        onClose: () => {},
      }),
    )

    try {
      act(() => findButtonByText(modalView.container, 'EDIT').click())
      expect(modalView.container.textContent).toContain('Save-Slot Scope')

      act(() => findButtonByText(modalView.container, 'Selected Save Slot(s)').click())
      const checkboxes = Array.from(
        modalView.container.querySelectorAll('input[type="checkbox"]'),
      ) as HTMLInputElement[]
      expect(checkboxes).toHaveLength(2)
      expect(checkboxes[0].checked).toBe(true)
      expect(checkboxes[1].checked).toBe(false)

      await act(async () => {
        findButtonByText(modalView.container, 'SAVE').click()
        await Promise.resolve()
      })
      expect(mockUpdateExpeditionScoped).toHaveBeenCalledWith(
        5,
        'Outer Arm Survey',
        'Initial',
        'selected',
        [10],
      )
      expect(mockUpdateExpedition).not.toHaveBeenCalled()
    } finally {
      modalView.unmount()
    }
  })
})
