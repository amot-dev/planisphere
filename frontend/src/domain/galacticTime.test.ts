import { describe, expect, it } from 'vitest'
import {
  edastroGecUrl,
  edsmSystemUrl,
  formatCredits,
  formatEfficiencyBonusLabel,
  formatEfficiencyBonusTooltip,
  formatElapsedDuration,
  formatLoss,
  formatLy,
  inaraSystemUrl,
  toGalacticTime,
} from './galacticTime'

describe('Galactic Standard Time (+1286 years)', () => {
  it('converts real ISO-8601 UTC to minute-rounded 33XX-MM-DD HH:mm UTC', () => {
    const iso = '2026-01-18T19:42:10Z'
    const gst = toGalacticTime(iso)
    expect(gst).toBe('3312-01-18 19:42 UTC')
    expect(toGalacticTime('2026-01-18T19:42:45Z')).toBe('3312-01-18 19:43 UTC')
  })

  it('preserves leap day 02-29 across the +1286 year shift', () => {
    const iso = '2028-02-29T08:15:00Z'
    const gst = toGalacticTime(iso)
    expect(gst).toBe('3314-02-29 08:15 UTC')
  })

  it('builds external system URLs for Inara, EDSM, and EDAstro GEC', () => {
    expect(inaraSystemUrl('Sagittarius A*')).toBe(
      'https://inara.cz/elite/starsystem/?search=Sagittarius%20A*',
    )
    expect(edsmSystemUrl('Beagle Point')).toBe(
      'https://www.edsm.net/en/system?systemName=Beagle%20Point',
    )
    expect(edastroGecUrl('Sol', 'https://edastro.com/gec/view/10')).toBe(
      'https://edastro.com/gec/view/10',
    )
    expect(edastroGecUrl('Sol')).toBe(
      'https://edastro.com/gec/search?q=Sol',
    )
  })

  it('formats credits and light years', () => {
    expect(formatCredits(95054000)).toBe('95,054,000 Cr')
    expect(formatLy(23016.8, 1)).toBe('23,016.8 Ly')
    expect(formatLoss(0)).toBe('0 Cr')
    expect(formatLoss(undefined)).toBe('0 Cr')
    expect(formatLoss(125000)).toBe('-125,000 Cr')
  })

  it('formats DSS efficiency bonus labels and tooltips', () => {
    expect(formatEfficiencyBonusLabel(0, 0)).toBe('Efficiency Bonus')
    expect(formatEfficiencyBonusTooltip(0, 0)).toBeUndefined()
    expect(formatEfficiencyBonusLabel(168, 184)).toBe('Efficiency Bonus (168 · 91%)')
    expect(formatEfficiencyBonusTooltip(168, 184)).toBe(
      '168 of 184 mapped bodies met the DSS probe efficiency target (91.3%)',
    )
  })

  it('formats elapsed durations across minutes, days, years, and live now fallback', () => {
    expect(formatElapsedDuration('')).toBe('')
    expect(formatElapsedDuration('2026-01-19T15:00:00Z', '2026-01-19T15:00:00Z')).toBe('0h 00m')
    expect(formatElapsedDuration('2026-01-19T15:00:00Z', '2026-01-19T16:35:45Z')).toBe('1h 35m')
    expect(formatElapsedDuration('2026-01-19T15:00:00Z', '2026-01-21T18:05:00Z')).toBe('2d 03h 05m')
    expect(formatElapsedDuration('2026-01-01T00:00:00Z', '2027-01-01T00:00:00Z')).toBe('1y 00h 00m')
    expect(formatElapsedDuration('2026-01-01T00:00:00Z', '2027-01-06T04:09:00Z')).toBe('1y 5d 04h 09m')
    expect(formatElapsedDuration('2026-01-19T16:00:00Z', '2026-01-19T15:00:00Z')).toBe('0h 00m')
    expect(
      formatElapsedDuration(
        '2026-01-19T15:00:00Z',
        '',
        Date.parse('2026-01-19T15:42:30Z'),
      ),
    ).toBe('0h 42m')
  })
})

