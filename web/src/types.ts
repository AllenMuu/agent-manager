export type Project = { id: string; name: string; path: string }
export type ErrorEnvelope = { error: { code: string; message: string } }
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
export type Finding = { path: string; message: string }
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
export type PlanChange = { path: string; action: string; detail?: string }
export type OperationPlan = {
  id: string
  projectId: string
  operation: string
  version: string
  resourceKind: string
  changes: PlanChange[]
  warnings: string[]
  createdAt: string
  expiresAt: string
  forceReplacementConfirmed: boolean
}
export type LatestOperation = {
  operation: string
  resourceKind: string
  at: string
  undoAvailable: boolean
  undoPreview?: { operation: string; changes: PlanChange[]; warnings: string[] }
} | null
