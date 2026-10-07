## 1. Distribution boundary
- [x] 1.1 Define approved npm wrapper design, capability contract and distribution-only network boundary.

## 2. npm wrapper
- [x] 2.1 Add failing regression tests before verified acquisition and command delegation implementation.
- [x] 2.2 Implement exact-version downloads, checksum verification, cleanup and lazy acquisition.
- [x] 2.3 Implement argument/stream/exit/signal delegation without intercepting install.

## 3. Release and documentation
- [x] 3.1 Add cross-platform GoReleaser builds and gated npm publication/retry workflow.
- [x] 3.2 Document npm-first installation, source fallback, credentials and publication procedure.

## 4. Validation
- [x] 4.1 Pass Node regressions, full Go tests/vet, release build and OpenSpec validation.
- [x] 4.2 Verify packed-package execution via isolated npm installation and npx.
