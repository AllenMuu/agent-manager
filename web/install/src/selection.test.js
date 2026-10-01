import { describe, expect, it } from 'vitest'
import { previewPayload } from './selection.js'

describe('previewPayload', () => {
  it('sends only the explicitly selected Skills and targets', () => {
    expect(previewPayload(['review', 'go-test'], ['codex'])).toEqual({
      skillIds: ['review', 'go-test'],
      targets: ['codex'],
      replaceConflicts: false,
    })
  })

  it('deduplicates the explicit selection and carries the replacement choice', () => {
    expect(previewPayload(['review', 'review'], ['pi', 'pi'], true)).toEqual({
      skillIds: ['review'],
      targets: ['pi'],
      replaceConflicts: true,
    })
  })
})
