export type SkillSelectionAction = 'none' | 'activate' | 'batch-install';

export function getSkillSelectionAction(skillIDs: readonly string[]): SkillSelectionAction {
  if (skillIDs.length === 0) return 'none';
  if (skillIDs.length === 1) return 'activate';
  return 'batch-install';
}

export function toggleSkillSelection(skillIDs: readonly string[], identifier: string): string[] {
  if (skillIDs.includes(identifier)) {
    return skillIDs.filter((skillID) => skillID !== identifier);
  }
  return [...skillIDs, identifier];
}
