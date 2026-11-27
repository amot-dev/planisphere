// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { makeSettings } from '../../test/fixtures'
import { mountTestRoot, setFormControlValue } from '../../test/render'
import { installWailsAppMock } from '../../test/wailsMock'
import { JournalDirectorySection } from './JournalDirectorySection'

describe('JournalDirectorySection', () => {
  it('renders directory status badge, browses, applies custom path, resets to default, and forwards RPC errors', async () => {
    const onSetError = vi.fn()
    const initialSettings = makeSettings({
      journalDir: '/initial/journals',
      journalDirExists: false,
    })

    const { spies } = installWailsAppMock({
      BrowseJournalDir: async () => '/picked/journals',
      SetJournalDir: async (targetDirectory: string) =>
        makeSettings({
          journalDir: targetDirectory || '/default/journals',
          journalDirExists: true,
        }),
    })

    const mounted = mountTestRoot(
      createElement(JournalDirectorySection, {
        settings: initialSettings,
        onSetError,
      }),
    )

    expect(mounted.container.textContent).toContain('NOT FOUND')
    const directoryInput = mounted.container.querySelector('input[type="text"]') as HTMLInputElement
    expect(directoryInput.value).toBe('/initial/journals')

    // Empty picker result keeps the current draft unchanged.
    spies.BrowseJournalDir.mockResolvedValueOnce('')
    await act(async () => {
      mounted.findButtonByText('BROWSE...').click()
      await Promise.resolve()
    })
    expect(directoryInput.value).toBe('/initial/journals')

    // Picked folder updates draft and APPLY persists it via SetJournalDir.
    await act(async () => {
      mounted.findButtonByText('BROWSE...').click()
      await Promise.resolve()
    })
    expect(directoryInput.value).toBe('/picked/journals')

    await act(async () => {
      mounted.findButtonByText('APPLY').click()
      await Promise.resolve()
    })
    expect(onSetError).toHaveBeenLastCalledWith('')
    expect(spies.SetJournalDir).toHaveBeenCalledWith('/picked/journals')

    // Manual edit followed by RESET TO DEFAULT invokes SetJournalDir with empty string.
    act(() => setFormControlValue(directoryInput, '/custom/manual'))
    await act(async () => {
      mounted.findButtonByText('RESET TO DEFAULT').click()
      await Promise.resolve()
    })
    expect(spies.SetJournalDir).toHaveBeenLastCalledWith('')
    expect(directoryInput.value).toBe('/default/journals')

    // Rerendering with updated settings reflects FOUND badge and synced path.
    mounted.rerender(
      createElement(JournalDirectorySection, {
        settings: makeSettings({ journalDir: '/synced/journals', journalDirExists: true }),
        onSetError,
      }),
    )
    expect(mounted.container.textContent).toContain('FOUND')
    expect(directoryInput.value).toBe('/synced/journals')

    // Browse, Apply, and Reset rejections propagate errors through onSetError.
    spies.BrowseJournalDir.mockRejectedValueOnce(new Error('browse dialog failed'))
    spies.SetJournalDir
      .mockRejectedValueOnce(new Error('invalid journal folder'))
      .mockRejectedValueOnce(new Error('reset failed'))

    await act(async () => {
      mounted.findButtonByText('BROWSE...').click()
      await Promise.resolve()
    })
    expect(onSetError).toHaveBeenLastCalledWith('Error: browse dialog failed')

    await act(async () => {
      mounted.findButtonByText('APPLY').click()
      await Promise.resolve()
    })
    expect(onSetError).toHaveBeenLastCalledWith('Error: invalid journal folder')

    await act(async () => {
      mounted.findButtonByText('RESET TO DEFAULT').click()
      await Promise.resolve()
    })
    expect(onSetError).toHaveBeenLastCalledWith('Error: reset failed')
  })
})
