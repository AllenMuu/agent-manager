import { describe, expect, it } from 'vitest';
import { getSkillSelectionAction, toggleSkillSelection } from './skillSelection';

describe('getSkillSelectionAction', () => {
  it('maps selection size to the available action', () => {
    expect(getSkillSelectionAction([])).toBe('none');
    expect(getSkillSelectionAction(['go-helper'])).toBe('install');
    expect(getSkillSelectionAction(['go-helper', 'go-test'])).toBe('install');
  });
});

describe('toggleSkillSelection', () => {
  it('adds and removes identifiers while preserving selection order', () => {
    expect(toggleSkillSelection([], 'go-helper')).toEqual(['go-helper']);
    expect(toggleSkillSelection(['go-helper'], 'go-helper')).toEqual([]);
    expect(toggleSkillSelection(['go-helper'], 'go-test')).toEqual(['go-helper', 'go-test']);
  });
});
