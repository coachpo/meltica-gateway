# Test Data and Fixtures

This directory contains additional external test applications, test data, and fixtures.

## Purpose

Store test resources that are:
- Shared across multiple test files
- Large test datasets or fixtures
- Integration test data
- Mock data for testing
- Test configuration files

## Structure

Organize by test type or component:
- `fixtures/` - Static test data files
- `testdata/` - Test input/output files
- `mocks/` - Mock service configurations
- `integration/` - Integration test resources

## Note

Unit tests should remain alongside the code they test (`*_test.go` files in package directories).
This directory is for additional test support files that don't fit within package directories.
