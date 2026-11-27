// @vitest-environment happy-dom
import { act, createElement } from 'react'
import { describe, expect, it } from 'vitest'
import { makeSettings } from '../../test/fixtures'
import { mountTestRoot } from '../../test/render'
import { installWailsAppMock } from '../../test/wailsMock'
import { APP_REPOSITORY, APP_VERSION } from '../../version'
import { VersionIndicator } from './VersionIndicator'

describe('VersionIndicator', () => {
  it('renders static up-to-date text and hollow offline indicator without external anchor links', () => {
    const upToDateView = mountTestRoot(
      createElement(VersionIndicator, {
        settings: makeSettings({
          latestVersion: `v${APP_VERSION}`,
          versionCheckFailed: false,
        }),
      }),
    )

    expect(upToDateView.container.querySelector('a')).toBeNull()
    const upToDateSpan = upToDateView.container.querySelector('span[title="Up to date"]')
    expect(upToDateSpan).not.toBeNull()
    expect(upToDateSpan?.textContent).toContain(`v${APP_VERSION}`)

    // GitHub check failure renders offline tooltip and hollow indicator even if latestVersion differs.
    upToDateView.rerender(
      createElement(VersionIndicator, {
        settings: makeSettings({
          latestVersion: '9.9.9',
          versionCheckFailed: true,
        }),
      }),
    )
    expect(upToDateView.container.querySelector('a')).toBeNull()
    const failedSpan = upToDateView.container.querySelector(
      'span[title="Unable to reach GitHub (version not checked)"]',
    )
    expect(failedSpan).not.toBeNull()
  })

  it('renders update-available release link on version mismatch and invokes OpenURL on click', () => {
    const { spies } = installWailsAppMock({
      OpenURL: async () => {},
    })

    const mounted = mountTestRoot(
      createElement(VersionIndicator, {
        settings: makeSettings({
          latestVersion: 'v9.4.0',
          versionCheckFailed: false,
        }),
      }),
    )

    const releaseAnchor = mounted.container.querySelector('a') as HTMLAnchorElement
    expect(releaseAnchor).not.toBeNull()
    expect(releaseAnchor.getAttribute('title')).toBe('v9.4.0 is available on GitHub')

    const expectedReleaseUrl = `${APP_REPOSITORY.replace(/\/$/, '')}/releases/latest`
    expect(releaseAnchor.getAttribute('href')).toBe(expectedReleaseUrl)

    const clickEvent = new MouseEvent('click', { bubbles: true, cancelable: true })
    act(() => {
      releaseAnchor.dispatchEvent(clickEvent)
    })
    expect(clickEvent.defaultPrevented).toBe(true)
    expect(spies.OpenURL).toHaveBeenCalledWith(expectedReleaseUrl)
  })
})
