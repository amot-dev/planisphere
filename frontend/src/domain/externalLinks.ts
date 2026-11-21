/**
 * BinaryExternalLinkMode represents an unconditional On or Off visibility
 * setting for external star system links such as Inara and GEC.
 */
export type BinaryExternalLinkMode = 'on' | 'off'

/**
 * EdsmExternalLinkMode represents the visibility setting for EDSM waypoint
 * links, supporting conditional display only when no GEC button is shown.
 */
export type EdsmExternalLinkMode = 'on' | 'if_no_gec' | 'off'

/**
 * ExternalLinkVisibilityConfig specifies the visibility modes for waypoint
 * external star-system links across Inara, EDSM, and EDAstro GEC.
 */
export interface ExternalLinkVisibilityConfig {
  inara?: string
  edsm?: string
  gec?: string
}

/**
 * normalizeBinaryMode coerces an Inara or GEC link visibility setting to 'on' or 'off'.
 */
export function normalizeBinaryMode(rawMode: string | undefined): BinaryExternalLinkMode {
  return rawMode === 'off' ? 'off' : 'on'
}

/**
 * normalizeEdsmMode coerces an EDSM link visibility setting to 'on', 'off', or 'if_no_gec'.
 */
export function normalizeEdsmMode(rawMode: string | undefined): EdsmExternalLinkMode {
  if (rawMode === 'on' || rawMode === 'off') {
    return rawMode
  }
  return 'if_no_gec'
}

/**
 * resolveExternalLinkVisibility evaluates configured external link rules for a
 * waypoint given whether an EDAstro GEC catalog entry URL exists.
 */
export function resolveExternalLinkVisibility(
  hasGecUrl: boolean,
  config?: ExternalLinkVisibilityConfig,
): { showInara: boolean; showEdsm: boolean; showGec: boolean } {
  const showInara = normalizeBinaryMode(config?.inara ?? 'on') === 'on'
  const showGec = normalizeBinaryMode(config?.gec ?? 'on') === 'on' && hasGecUrl
  const normalizedEdsmMode = normalizeEdsmMode(config?.edsm ?? 'if_no_gec')
  const showEdsm =
    normalizedEdsmMode === 'on'
      ? true
      : normalizedEdsmMode === 'off'
        ? false
        : !showGec

  return { showInara, showEdsm, showGec }
}

