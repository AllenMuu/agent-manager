export function previewPayload(skillIDs, targets, replaceConflicts = false) {
  return {
    skillIds: [...new Set(skillIDs)],
    targets: [...new Set(targets)],
    replaceConflicts,
  }
}
