import { describe, expect, it } from 'vitest'
import {
  edastroGecUrl,
  edsmSystemUrl,
  formatCredits,
  formatLoss,
  formatLy,
  fromGalacticTime,
  inaraSystemUrl,
  toGalacticTime,
} from './galacticTime'

describe('Galactic Standard Time (+1286 years)', () => {
  it('converts real ISO-8601 UTC to minute-rounded 33XX-MM-DD HH:mm UTC and back', () => {
    const iso = '2026-01-18T19:42:10Z'
    const gst = toGalacticTime(iso)
    expect(gst).toBe('3312-01-18 19:42 UTC')
    expect(fromGalacticTime(gst)).toBe('2026-01-18T19:42:00Z')
    expect(toGalacticTime('2026-01-18T19:42:45Z')).toBe('3312-01-18 19:43 UTC')
  })

  it('preserves leap day 02-29 across the +1286 year shift', () => {
    const iso = '2028-02-29T08:15:00Z'
    const gst = toGalacticTime(iso)
    expect(gst).toBe('3314-02-29 08:15 UTC')
    expect(fromGalacticTime(gst)).toBe(iso)
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
})

