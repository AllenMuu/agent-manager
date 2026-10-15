export type Project = { id: string; name: string; path: string }
export type ErrorEnvelope = { error: { code: string; message: string } }
export type MessageRef = { key: string; args?: Record<string, string> }
export type Agent = {
  id: string
  status: string
  availability: string
  resourceKinds: string[]
  capabilities: string[]
}
export type Recommendation = {
  identifier: string
  description: string
  matchReason: string
  matchReasonRef?: MessageRef
  matchingEvidence?: { technology: string; field: string }[]
  confidence: string
  score: number
}
export type Technology = { id: string; label: string; category: string }
export type Evidence = { path: string; technology: string }
export type Resource = {
  target: string
  identifier: string
  path: string
  sourcePath?: string
  status: string
}
export type Finding = {
  path: string
  message: string
  code?: string
  messageRef?: MessageRef
  repairAction?: 'reconcile' | 'add_gitignore'
  guidance?: string
  guidanceRef?: MessageRef
}
export type Scope = {
  path: string
  status: string
  technologies: Technology[]
  evidence: Evidence[]
  diagnostics: Finding[]
  recommendations: Recommendation[]
  nextAction: string
}
export type RecommendationResult = {
  project: string
  scanComplete: boolean
  diagnostics: Finding[]
  scopes: Scope[]
}
export type Skill = {
  identifier: string
  name: string
  description: string
  tags: string[]
  compatibility: string[]
  provenance?: string
  body?: string
}
export type SubAgent = {
  version: string
  id: string
  name: string
  role: string
  instructions: string
  skills?: string[]
  compatibility?: { agents?: string[] }
  requiredCapabilities?: string[]
}
export type SubAgentDiagnostic = { id?: string; path: string; message: string }
export type PlanChange = { path: string; action: string; actionRef?: MessageRef; detail?: string }
export type OperationPlan = {
  id: string
  projectId: string
  operation: string
  operationRef?: MessageRef
  version: string
  resourceKind: string
  changes: PlanChange[]
  warnings: string[]
  warningRefs?: (MessageRef | null)[]
  createdAt: string
  expiresAt: string
  forceReplacementConfirmed: boolean
}
export type LatestOperation = {
  operation: string
  operationRef?: MessageRef
  resourceKind: string
  at: string
  undoAvailable: boolean
  undoPreview?: { operation: string; operationRef?: MessageRef; changes: PlanChange[]; warnings: string[]; warningRefs?: (MessageRef | null)[] }
} | null

export type SyncMode = 'auto' | 'symlink-only' | 'copy-only'
export type SyncSource = {
  id: string
  identifier: string
  scope: 'global' | 'project'
  location: string
  path: string
  checksum?: string
  eligible: boolean
  problem?: string
  alreadyManaged?: boolean
  managedMaterial?: string
}
export type SyncSourceGroup = {
  id: string
  identifier: string
  checksum: string
  versionKey: string
  sources: SyncSource[]
  alreadyPresent: boolean
  conflict: boolean
  conflictReason?: string
  existingCurrent?: string
  requiresCurrentChoice?: boolean
  makeCurrent?: boolean
}
export type SyncLocation = { scope: 'global' | 'project'; label: string; path: string }
export type SyncDiagnostic = { scope: 'global' | 'project'; path: string; message: string }
export type SyncInventory = {
  repositoryPath: string
  locations: SyncLocation[]
  sources: SyncSource[]
  groups: SyncSourceGroup[]
  diagnostics: SyncDiagnostic[]
  globalResyncUndoAvailable: boolean
  projectResyncUndoAvailable: boolean
}
export type SyncImportPreview = {
  repositoryPath: string
  groups: SyncSourceGroup[]
  selectedGroupIds: string[]
  currentChoices?: Record<string, string>
  currentChoiceRequired?: string[]
  repositoryStates: Record<string, string>
  confirmationToken: string
}
export type SyncPlan = { id: string; scope?: string; plan: { operation: string; changes: PlanChange[]; warnings: string[] } }
