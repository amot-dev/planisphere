/**
 * Detected format of user-provided bulk waypoint import input.
 */
export type WaypointImportFormat = 'empty' | 'plain' | 'spansh-csv'

/**
 * ParsedWaypointImport holds the extracted star system names and summary counts
 * from a plain-text system list or Spansh CSV route export.
 */
export interface ParsedWaypointImport {
  format: WaypointImportFormat
  systems: string[]
  collapsedDuplicateCount: number
  skippedLineCount: number
}

function normalizeSystemName(rawName: string): string {
  return rawName.trim().replace(/\s+/g, ' ')
}

/**
 * parseCsvLine parses a single RFC 4180 comma-separated line, preserving commas
 * inside quoted fields and unescaping doubled quotes.
 */
function parseCsvLine(line: string): string[] {
  const fields: string[] = []
  let currentField = ''
  let insideQuotes = false

  for (let index = 0; index < line.length; index++) {
    const character = line[index]
    if (insideQuotes) {
      if (character === '"') {
        if (line[index + 1] === '"') {
          currentField += '"'
          index++
        } else {
          insideQuotes = false
        }
      } else {
        currentField += character
      }
      continue
    }

    if (character === '"') {
      insideQuotes = true
    } else if (character === ',') {
      fields.push(currentField)
      currentField = ''
    } else {
      currentField += character
    }
  }
  fields.push(currentField)
  return fields
}

function findSpanshSystemColumnIndex(headerFields: string[]): number {
  const normalizedHeaders = headerFields.map((field) => field.trim().toLowerCase())
  const systemNameIndex = normalizedHeaders.indexOf('system name')
  if (systemNameIndex >= 0) {
    return systemNameIndex
  }
  if (normalizedHeaders.length > 1) {
    return normalizedHeaders.indexOf('system')
  }
  return -1
}

function parseSpanshCsvLines(
  lines: string[],
  headerLineIndex: number,
  systemColumnIndex: number,
): ParsedWaypointImport {
  const systems: string[] = []
  let collapsedDuplicateCount = 0
  let skippedLineCount = 0
  let previousSystemKey = ''

  for (let lineIndex = headerLineIndex + 1; lineIndex < lines.length; lineIndex++) {
    const trimmedLine = lines[lineIndex].trim()
    if (!trimmedLine) {
      continue
    }
    const fields = parseCsvLine(lines[lineIndex])
    const systemName = normalizeSystemName(fields[systemColumnIndex] ?? '')
    if (!systemName) {
      skippedLineCount++
      continue
    }
    const systemKey = systemName.toLowerCase()
    if (systemKey === previousSystemKey) {
      collapsedDuplicateCount++
      continue
    }
    previousSystemKey = systemKey
    systems.push(systemName)
  }

  return {
    format: 'spansh-csv',
    systems,
    collapsedDuplicateCount,
    skippedLineCount,
  }
}

function cleanPlainTextLine(rawLine: string): string {
  let cleanedLine = rawLine.trim()
  if (cleanedLine.length >= 2 && cleanedLine.startsWith('"') && cleanedLine.endsWith('"')) {
    cleanedLine = cleanedLine.slice(1, -1).replace(/""/g, '"').trim()
  }
  // Only strip explicit punctuation-delimited list markers so digit-leading
  // star systems such as "3 Geminorum" or "2MASS J02351897+6131236" stay intact.
  cleanedLine = cleanedLine.replace(/^(?:\d+[.)]|[-*])\s+/, '')
  return normalizeSystemName(cleanedLine)
}

function parsePlainTextLines(lines: string[]): ParsedWaypointImport {
  const systems: string[] = []
  let skippedLineCount = 0

  for (const rawLine of lines) {
    const trimmedLine = rawLine.trim()
    if (!trimmedLine) {
      continue
    }
    if (trimmedLine.startsWith('#')) {
      skippedLineCount++
      continue
    }
    const systemName = cleanPlainTextLine(trimmedLine)
    if (!systemName) {
      skippedLineCount++
      continue
    }
    systems.push(systemName)
  }

  return {
    format: 'plain',
    systems,
    collapsedDuplicateCount: 0,
    skippedLineCount,
  }
}

/**
 * parseWaypointImportText parses either a plain newline-separated list of star
 * systems or a Spansh CSV export into an ordered array of normalized system names.
 */
export function parseWaypointImportText(rawText: string): ParsedWaypointImport {
  const normalizedText = rawText.replace(/^\uFEFF/, '').replace(/\r\n?/g, '\n')
  if (!normalizedText.trim()) {
    return {
      format: 'empty',
      systems: [],
      collapsedDuplicateCount: 0,
      skippedLineCount: 0,
    }
  }

  const lines = normalizedText.split('\n')
  const firstNonEmptyLineIndex = lines.findIndex((line) => line.trim() !== '')
  if (firstNonEmptyLineIndex < 0) {
    return {
      format: 'empty',
      systems: [],
      collapsedDuplicateCount: 0,
      skippedLineCount: 0,
    }
  }

  const headerFields = parseCsvLine(lines[firstNonEmptyLineIndex])
  const systemColumnIndex = findSpanshSystemColumnIndex(headerFields)
  if (systemColumnIndex >= 0) {
    return parseSpanshCsvLines(lines, firstNonEmptyLineIndex, systemColumnIndex)
  }

  return parsePlainTextLines(lines)
}

/**
 * deriveExpeditionNameFromFilename strips a trailing file extension from a
 * loaded import filename so it can pre-fill an empty Expedition Name input.
 */
export function deriveExpeditionNameFromFilename(fileName: string): string {
  const trimmedName = fileName.trim()
  if (!trimmedName) {
    return ''
  }
  const lastDotIndex = trimmedName.lastIndexOf('.')
  if (lastDotIndex <= 0) {
    return trimmedName
  }
  return trimmedName.slice(0, lastDotIndex).trim()
}

