# Repository Guidelines

## Project Structure & Module Organization

This is a Go 1.23 module (`github.com/DaviMGDev/memento`). Runtime packages are grouped by responsibility: `context/` contains typed context, bindings, and effects; `runtime/` implements fibers, scheduling, and lifecycle; `loader/` handles declarative entries and reconciliation. `conformance/` contains the Godog runner and property checks, while the corresponding behavioral specification is in `specs/SPEC.md` and scenarios in `specs/features/`. Runnable examples live under `examples/` (currently `examples/composition`). Keep package-specific tests beside the code they exercise.

## Build, Test, and Development Commands

- `go run ./examples/composition` runs the end-to-end composition example.
- `go vet ./...` checks for common Go mistakes; CI runs this on every push and pull request.
- `go test ./... -race` runs unit, conformance, and property tests with race detection; this is the CI test command.
- `gofmt -w <files>` formats changed Go files. Format Go code before committing.

## Coding Style & Naming Conventions

Follow standard Go formatting and idioms: tabs are applied by `gofmt`, exported identifiers use Go-style `MixedCaps`, and package names stay short and lowercase. Keep APIs and comments aligned with the behavioral contract in `specs/SPEC.md`. Name Go tests `TestFeature` and keep Gherkin scenarios in descriptive `.feature` files under `specs/features/`.

## Testing Guidelines

Add focused `_test.go` coverage in the owning package for behavior changes. Changes to lifecycle, effects, coeffects, or configuration should also update or extend the matching feature file and its conformance steps when appropriate. Preserve the project's guarantees with race-enabled tests; no separate coverage threshold is configured.

## Commit Convention

Commits follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <description>
```

The scope is optional. A breaking change adds `!` after the type or scope and a `BREAKING CHANGE:` footer describing the migration.

### Types

| Type | Use for |
|------|---------|
| `feat` | New behavior or capability |
| `fix` | Bug fix |
| `refactor` | Restructuring with no behavior change |
| `perf` | Performance improvement with no behavior change |
| `test` | Unit, conformance, and property tests |
| `docs` | `README.md`, `specs/`, or other documentation |
| `ci` | CI configuration (`.github/workflows/`) |
| `chore` | Tooling, module metadata, and dependencies |
| `revert` | Reverting a previous commit |

Do not use `style` or `build`: formatting changes are folded into the functional commit, and build-system changes are `ci` or `chore`.

### Scopes

Scope names the package or area the commit primarily touches:

- `context`, `runtime`, `loader` — runtime packages
- `conformance` — Godog runner and property checks
- `specs` — `SPEC.md`, feature files, and `specs/log.md`
- `examples` — runnable examples

Omit the scope for repo-wide changes, as in `docs: add the README quickstart` or `ci: vet and race-test every push and pull request`. When a new package appears, add its scope to this list in the commit that introduces it.

### Rules

- **Description**: imperative mood, lowercase first letter, no trailing period, 72 characters or fewer; name the behavior, not the file — `feat(loader): reconcile entries per changed field`.
- **Body**: wrap at 72 columns and explain what changed and why; reference specification decisions (`D8`), feature files, or issues (`Refs #12`). Add a body whenever the motivation is not obvious from the description.
- **Footers**: `Token: value` or `Token #value`, such as `Fixes: #12` or `BREAKING CHANGE: ...`.
- **Atomicity**: one logical change and one type per commit. Do not mix unrelated features, fixes, tests, and documentation; if the description needs "and", split the commit.
- **Formatting**: run `gofmt -w` on changed Go files so formatting lands in the functional commit, not a separate commit.

## Pull Request Guidelines

Pull requests should explain the behavior and rationale, link relevant issues or specification decisions, and include test results (`go vet ./...`, `go test ./... -race`). Use the same Conventional Commit form for the PR title. Include example output or screenshots only when they clarify a user-visible change.

## Security & Configuration

Keep credentials and machine-specific configuration out of the repository. The project currently has no documented environment-variable setup; describe any new configuration in the README and cover its behavior in tests.
