// @vitest-environment happy-dom
import { act, createElement, type ReactElement } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { app, expedition } from '../../../api/models'

const mockBrowseWaypointImportFile = vi.fn<() => Promise<app.WaypointImportFile>>()
const mockCreateExpedition = vi.fn()
const mockCreateExpeditionWithWaypoints = vi.fn()

vi.mock('../../../api/backend', () => ({
  BrowseWaypointImportFile: () => mockBrowseWaypointImportFile(),
  CloneExpedition: vi.fn(),
  CreateExpedition: (...args: unknown[]) => mockCreateExpedition(...args),
  CreateExpeditionWithWaypoints: (...args: unknown[]) => mockCreateExpeditionWithWaypoints(...args),
  DeleteExpedition: vi.fn(),
  UpdateExpedition: vi.fn(),
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
  const button = Array.from(container.querySelectorAll('button')).find((element) =>
    element.textContent?.includes(textMatch),
  )
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

describe('WaypointImportSection', () => {
  it('omits format badges while preserving waypoint count and duplicate collapse summary', () => {
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

    const plainListView = mountElement(
      createElement(WaypointImportSection, {
        rawImportText: 'Sol',
        parsedImport: {
          format: 'plain',
          systems: ['Sol'],
          collapsedDuplicateCount: 0,
          skippedLineCount: 0,
        },
        loadedFileNote: '',
        loadingFile: false,
        disabled: false,
        onChangeText: () => {},
        onLoadFile: () => {},
        onClear: () => {},
      }),
    )

    try {
      expect(plainListView.container.textContent).toContain('1 WAYPOINT READY')
      expect(plainListView.container.textContent).not.toContain('SPANSH CSV')
      expect(plainListView.container.textContent).not.toContain('PLAIN LIST')
    } finally {
      plainListView.unmount()
    }
  })
})

describe('ExpeditionManageModal', () => {
  afterEach(() => {
    vi.clearAllMocks()
  })

  it('disables expedition creation while a waypoint import file is loading and enables it after load completes', async () => {
    let resolveImportFile!: (file: app.WaypointImportFile) => void
    mockBrowseWaypointImportFile.mockImplementation(
      () =>
        new Promise<app.WaypointImportFile>((resolve) => {
          resolveImportFile = resolve
        }),
    )
    mockCreateExpeditionWithWaypoints.mockResolvedValue(
      expedition.Expedition.createFrom({
        id: 42,
        name: 'Distant Worlds',
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
      act(() => {
        setInputValue(nameInput, 'Distant Worlds')
      })

      const createButtonBeforeLoad = findButtonByText(modalView.container, 'CREATE EXPEDITION')
      expect(createButtonBeforeLoad.disabled).toBe(false)

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
      expect(mockCreateExpeditionWithWaypoints).not.toHaveBeenCalled()

      await act(async () => {
        resolveImportFile(
          app.WaypointImportFile.createFrom({
            fileName: 'spansh-route.csv',
            content: '"System Name","Jumps"\n"Sol","0"\n"Colonia","1"\n',
          }),
        )
        await Promise.resolve()
      })

      expect(modalView.container.textContent).toContain('2 WAYPOINTS READY')

      const createAndImportButton = findButtonByText(modalView.container, 'CREATE AND IMPORT (2)')
      expect(createAndImportButton.disabled).toBe(false)
      expect(createAndImportButton.className).not.toContain('cursor-not-allowed')

      await act(async () => {
        createAndImportButton.click()
        await Promise.resolve()
      })

      expect(mockCreateExpeditionWithWaypoints).toHaveBeenCalledWith('Distant Worlds', '', [
        'Sol',
        'Colonia',
      ])
      expect(onSelect).toHaveBeenCalledWith(42)
      expect(onClose).toHaveBeenCalledTimes(1)
    } finally {
      modalView.unmount()
    }
  })
})
