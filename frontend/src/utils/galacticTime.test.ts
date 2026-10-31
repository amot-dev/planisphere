import { describe, expect, it } from 'vitest'
import {
  edastroGecUrl,
  edsmSystemUrl,
  formatCredits,
  formatLy,
  fromGalacticTime,
  inaraSystemUrl,
  toGalacticTime,
} from './galacticTime'

describe('Galactic Standard Time (+1286 years)', () => {
  it('converts real ISO-8601 UTC to 33XX-MM-DD HH:mm:ss UTC and back', () => {
    const iso = '2026-01-18T19:42:10Z'
    const gst = toGalacticTime(iso)
    expect(gst).toBe('3312-01-18 19:42:10 UTC')
    expect(fromGalacticTime(gst)).toBe(iso)
  })

  it('preserves leap day 02-29 across the +1286 year shift', () => {
    const iso = '2028-02-29T08:15:00Z'
    const gst = toGalacticTime(iso)
    expect(gst).toBe('3314-02-29 08:15:00 UTC')
    expect(fromGalacticTime(gst)).toBe(iso)
  })

  it('builds external system URLs for Inara, EDSM, and EDAstro GEC', () => {
    expect(inaraSystemUrl('Sagittarius A*')).toBe(
      'https://inara.cz/elite/starsystem/?search=Sagittarius%20A*',
    )
    expect(edsmSystemUrl('Beagle Point')).toBe(
      'https://www.edsm.net/en/system?systemName=Beagle%20Point',
    )
    expect(edastroGecUrl('Sol', 10477373803, 'https://edastro.com/gec/view/10')).toBe(
      'https://edastro.com/gec/view/10',
    )
    expect(edastroGecUrl('Sol', 10477373803)).toBe(
      'https://edastro.com/gec/search?q=Sol',
    )
  })

  it('formats credits and light years', () => {
    expect(formatCredits(95054000)).toBe('95,054,000 Cr')
    expect(formatLy(23016.8, 1)).toBe('23,016.8 Ly')
  })
})

