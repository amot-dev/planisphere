export { app, expedition, exporter, modes, realization, replay, stats } from '../../wailsjs/go/models'

/**
 * DataChangedPayload matches the Go app.DataChanged payload emitted on Wails
 * data:changed runtime notifications.
 */
export interface DataChangedPayload {
  domains?: string[]
  expeditionId?: number
}

/**
 * EddImportProgress describes live progress events emitted while importing
 * historical journal entries from an external EDDiscovery SQLite database.
 */
export interface EddImportProgress {
  active: boolean
  stage: string
  current: number
  total: number
  percent: number
  files: number
}

