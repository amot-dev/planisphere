import { describe, expect, it } from 'vitest'
import {
  deriveExpeditionNameFromFilename,
  parseWaypointImportText,
} from './waypointImport'

describe('parseWaypointImportText', () => {
  it('returns empty state for blank input', () => {
    expect(parseWaypointImportText('')).toEqual({
      format: 'empty',
      systems: [],
      collapsedDuplicateCount: 0,
      skippedLineCount: 0,
    })
    expect(parseWaypointImportText('   \r\n\t\n  ')).toEqual({
      format: 'empty',
      systems: [],
      collapsedDuplicateCount: 0,
      skippedLineCount: 0,
    })
  })

  it('parses plain system lists, preserves digit-leading names and loop revisits, and skips blanks and comments', () => {
    const rawList = [
      '# Inner Orion Spur',
      '  Shinrarta Dezhra  ',
      '',
      '1. Omega Sector VE-Q b5-15',
      '02) 3 Geminorum',
      '- 2MASS J02351897+6131236',
      '*   Scheau   Prue SE-Q e5-770 ',
      '"Sagittarius A*"',
      '',
      'Shinrarta Dezhra',
    ].join('\r\n')

    expect(parseWaypointImportText(rawList)).toEqual({
      format: 'plain',
      systems: [
        'Shinrarta Dezhra',
        'Omega Sector VE-Q b5-15',
        '3 Geminorum',
        '2MASS J02351897+6131236',
        'Scheau Prue SE-Q e5-770',
        'Sagittarius A*',
        'Shinrarta Dezhra',
      ],
      collapsedDuplicateCount: 0,
      skippedLineCount: 1,
    })
  })

  it('extracts System Name from Spansh Neutron Plotter CSV with BOM and CRLF', () => {
    const spanshCsv =
      '\uFEFF"System Name","Distance To Arrival","Distance Remaining","Neutron Star","Jumps"\r\n' +
      '"Sol","0","22000.47","No","0"\r\n' +
      '"Jackson\'s Lighthouse","137.45","21863.02","Yes","3"\r\n' +
      '"Colonia","22000.47","0","No","1"\r\n'

    expect(parseWaypointImportText(spanshCsv)).toEqual({
      format: 'spansh-csv',
      systems: ['Sol', "Jackson's Lighthouse", 'Colonia'],
      collapsedDuplicateCount: 0,
      skippedLineCount: 0,
    })
  })

  it('collapses consecutive duplicate systems in Spansh multi-body CSVs while preserving non-consecutive revisits', () => {
    const roadToRichesCsv = [
      '"System Name","Body Name","Body Subtype","Distance To Arrival"',
      '"Sol","Sol 3","Earth-like world","499"',
      '"sol","Sol 4","Terraformable, rocky","750"',
      '"Epsilon Indi","Mitterand Hollow","Rocky body","120"',
      '"Sol","Sol 2","High metal content","360"',
    ].join('\n')

    expect(parseWaypointImportText(roadToRichesCsv)).toEqual({
      format: 'spansh-csv',
      systems: ['Sol', 'Epsilon Indi', 'Sol'],
      collapsedDuplicateCount: 1,
      skippedLineCount: 0,
    })
  })

  it('handles header-only or comment-only inputs cleanly with zero systems', () => {
    expect(parseWaypointImportText('"System Name","Jumps"\n')).toEqual({
      format: 'spansh-csv',
      systems: [],
      collapsedDuplicateCount: 0,
      skippedLineCount: 0,
    })
    expect(parseWaypointImportText('# only a comment\n# second comment')).toEqual({
      format: 'plain',
      systems: [],
      collapsedDuplicateCount: 0,
      skippedLineCount: 2,
    })
  })
})

describe('deriveExpeditionNameFromFilename', () => {
  it('strips trailing extensions and preserves base names', () => {
    expect(deriveExpeditionNameFromFilename('Xibalba Expedition.txt')).toBe('Xibalba Expedition')
    expect(deriveExpeditionNameFromFilename('spansh-route.csv')).toBe('spansh-route')
    expect(deriveExpeditionNameFromFilename('route.v2.csv')).toBe('route.v2')
    expect(deriveExpeditionNameFromFilename('no-extension')).toBe('no-extension')
    expect(deriveExpeditionNameFromFilename('  ')).toBe('')
  })
})

