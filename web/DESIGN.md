# Local Console Design

## 1. Product feel

Build a quiet operator workspace: useful, local, legible, and a little tactile. It should feel closer to a well-made field notebook than a marketing dashboard. Status, evidence, and the next safe action always outrank decoration.

## 2. Visual direction

Use warm paper surfaces with graphite text, muted olive status, and clay-orange for the one primary action on a view. Thin rules and small uppercase labels provide structure. Avoid gradients, glass effects, giant hero numbers, and decorative charts.

## 3. Color roles

- Canvas: `#f4f1e9`; raised paper: `#fffdf8`; secondary panel: `#ece8de`.
- Main text: `#252923`; supporting text: `#696d64`; quiet rule: `#d9d4c8`.
- Primary action: `#b84f31`; primary hover: `#963b24`; action text: `#fffaf3`.
- Ready: `#376b4d`; warning: `#9a6426`; error: `#a43b31`.
- Focus: `#185f70` with a three-pixel outline and two-pixel offset.

## 4. Type and information hierarchy

Use system sans-serif stacks with `-apple-system`, `BlinkMacSystemFont`, `Segoe UI`, `PingFang SC`, and `Noto Sans CJK SC` fallbacks. Keep headings short and sentence-case. Metadata labels are compact and tracked; paths and identifiers use a system monospace face. Body copy should stay at 14–16px with comfortable line-height.

## 5. Layout and spacing

At desktop widths, use a 228px navigation rail and a flexible content column capped at 1120px. The shell has a compact masthead, a clear page title, then grouped content. Use an 8px spacing rhythm; reserve larger 24px and 32px gaps for section changes. Lists align labels, status, and actions consistently.

## 6. Surfaces and geometry

Cards use a one-pixel border, 10px radius, and no floating shadow unless a review panel overlays the page. Buttons and inputs have 8px radii. Status is conveyed by text plus color. Long Skill and SubAgent text stays in a readable document column; paths wrap rather than widening the page.

## 7. Interaction states

Every network view has loading, empty, and recoverable error states. Review plans show the exact filesystem changes before a separate execute action. Force replacement is a distinct checkbox and appears again in the plan summary. Cancel is always available while a plan is pending. No background mutation or silent auto-confirmation is allowed.

## 8. Responsive behavior

Below 760px, move navigation above the workspace and wrap it into a compact grid. Stack cards and forms into one column, let long paths wrap, and keep primary actions full-width. Maintain at least 40px control height. The page itself must not scroll horizontally at 375px.

## 9. Accessibility and motion

Use landmarks, headings, labels, buttons, lists, and tables according to their meaning. Keep a visible focus ring, preserve logical keyboard order, announce request status with live regions, and never rely on color alone. Reduce motion to a short press response; disable even that response under `prefers-reduced-motion: reduce`.
