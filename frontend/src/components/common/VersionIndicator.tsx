import { OpenURL } from '../../../wailsjs/go/app/App'
import { app } from '../../../wailsjs/go/models'
import { APP_REPOSITORY, APP_VERSION } from '../../version'

/**
 * VersionIndicatorProps defines the application settings used to determine
 * the GitHub release version status in the top bar.
 */
export interface VersionIndicatorProps {
  settings: app.Settings | null
}

/**
 * VersionIndicator renders the unboxed application version number at the same
 * height as the product title and a status indicator to the right:
 * - Mismatched version: light blue version link and solid blue indicator
 * - GitHub API unreachable: hollow muted bronze indicator
 * - Up to date: muted bronze version with no indicator
 */
export function VersionIndicator({ settings }: VersionIndicatorProps) {
  const versionCheckFailed = Boolean(settings?.versionCheckFailed)
  const latestVersion = (settings?.latestVersion ?? '').trim().replace(/^v/i, '')
  const hasVersionMismatch =
    !versionCheckFailed &&
    latestVersion !== '' &&
    latestVersion !== APP_VERSION
  const versionTooltip = versionCheckFailed
    ? 'Unable to reach GitHub (version not checked)'
    : hasVersionMismatch
      ? `v${latestVersion} is available on GitHub`
      : 'Up to date'

  const latestReleaseUrl = `${APP_REPOSITORY.replace(/\/$/, '')}/releases/latest`

  if (hasVersionMismatch) {
    return (
      <a
        href={latestReleaseUrl}
        onClick={(event) => {
          event.preventDefault()
          OpenURL(latestReleaseUrl).catch(console.error)
        }}
        className="inline-flex items-center gap-1.5 font-mono-tabular text-[11px] font-semibold text-[#00D2FF] hover:underline tracking-normal leading-none select-none cursor-pointer"
        title={versionTooltip}
      >
        <span>v{APP_VERSION}</span>
        <span className="w-1.5 h-1.5 bg-[#00D2FF] shrink-0" />
      </a>
    )
  }

  return (
    <span
      className="inline-flex items-center gap-1.5 font-mono-tabular text-[11px] font-semibold text-[#a6907c] tracking-normal leading-none select-none"
      title={versionTooltip}
    >
      <span>v{APP_VERSION}</span>
      {versionCheckFailed ? (
        <span className="w-1.5 h-1.5 border border-[#a6907c] shrink-0" />
      ) : null}
    </span>
  )
}
