## 1. Baseline and reconciliation

- [x] 1.1 Refresh remote main, isolate the task, record ownership and preserve unrelated changes.
- [x] 1.2 Run the complete Go baseline suite.
- [x] 1.3 Confirm public test seams and map R1-R9 to current implementation and validation.

## 2. Remaining defects, one red-green slice at a time

- [x] 2.1 R2: initialization rollback preserves concurrent unmanaged files and restores its published paths safely.
- [x] 2.2 R6: Git guidance treats managed paths literally and refuses line-injection names before changes.
- [x] 2.3 R7: documented home-relative configuration resolves to the current user's home.
- [x] 2.4 R9: nested marker evidence reaches the nearest containing scope with correct paths and isolation.

## 3. Verification and review

- [x] 3.1 Verify R1/R3/R4/R5/R8 against current baseline and add any necessary regression coverage.
- [x] 3.2 Run full Go tests, race checks, vet/build and OpenSpec validation.
- [x] 3.3 Freeze the diff, perform independent review, address actionable findings with regression tests, and rerun checks.
- [x] 3.4 Save the per-finding acceptance matrix and report exact delivery status.
