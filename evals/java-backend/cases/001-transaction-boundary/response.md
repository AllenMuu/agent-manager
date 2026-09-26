The transaction boundary belongs in the service operation, and after-commit behavior
must be considered separately from the database transaction. The implementation
should preserve the existing framework version while addressing the boundary.
