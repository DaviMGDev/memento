# Contributing to memento

Thanks for your interest in memento. This is an independent Go implementation of
spatiotemporal composability, and it is **spec-first**: the behavioral contract
lives in [`specs/`](specs/) and the implementation follows it. Before changing
behavior, change the contract.

## Getting started

Requires [Go 1.23+](https://go.dev/dl/).

```console
$ git clone https://github.com/DaviMGDev/memento.git
$ cd memento
$ go run ./examples/composition   # end-to-end sanity check
$ go vet ./...                    # CI runs this on every push and PR
$ go test ./... -race             # unit, conformance, and property tests
```

`go test ./... -race` is the CI test command. It runs the unit suites, all
Gherkin scenarios via Godog, and the property harness under the race detector.

## Project layout

| Path | Contents |
|---|---|
| `context/` | Typed keys, the context tree, bindings, revertible effects, per-key equivalence |
| `runtime/` | Fibers, scheduler, reactive coeffects, lifecycle, registry introspection |
| `loader/` | Declarative entries, component factories, per-field reconciliation |
| `conformance/` | Godog runner and the shipped property harness |
| `specs/SPEC.md` | The behavioral specification and its Decisions (`D1`–`D8`) |
| `specs/features/` | Gherkin scenarios — the executable contract |
| `examples/` | Runnable examples |

## Spec-first workflow

1. **Read the contract.** Start with [`specs/SPEC.md`](specs/SPEC.md); the
   Decisions section explains *why* the semantics are what they are.
2. **Change the spec.** For behavior changes, update `SPEC.md` and/or the
   matching `specs/features/*.feature` file first. If you are settling a design
   question, record it as a new `D`-numbered decision with its trade-off.
3. **Implement.** Keep packages aligned with the vocabulary of the
   specification — the API should read like the spec.
4. **Verify.** Extend the Gherkin scenario and its conformance steps, and add
   focused `_test.go` coverage in the owning package.

Changes to lifecycle, effects, coeffects, or configuration should update or
extend the matching feature file and its conformance steps.

## Testing guidelines

- Put `_test.go` files beside the code they exercise.
- Name Go tests `TestFeature`; keep Gherkin scenarios descriptive and in the
  feature file for the affected behavior.
- Preserve the project's guarantees: `go test ./... -race` must stay green,
  and the property harness (effect witness, coeffect commutativity, LIFO
  reversal, randomized reconciliation convergence) must keep passing.
- There is no coverage threshold — tests should pin behavior, not chase a
  number.

## Coding style

- Standard Go: `gofmt`-clean, tabs, short lowercase package names,
  `MixedCaps` for exported identifiers. Run `gofmt -w` on changed files before
  committing.
- Keep exported APIs and comments consistent with `specs/SPEC.md`.
- The `context` and `runtime` package names shadow the standard library; use
  import aliases where both are in scope (see `D8` in `SPEC.md`).

## Commit conventions

Commits follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <description>
```

The scope is optional. A breaking change adds `!` after the type or scope and a
`BREAKING CHANGE:` footer describing the migration.

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

Do not use `style` or `build`: formatting changes are folded into the functional
commit, and build-system changes are `ci` or `chore`.

### Scopes

Scope names the package or area the commit primarily touches:

- `context`, `runtime`, `loader` — runtime packages
- `conformance` — Godog runner and property checks
- `specs` — `SPEC.md`, feature files, and `specs/log.md`
- `examples` — runnable examples

Omit the scope for repo-wide changes, as in `docs: add the README quickstart` or
`ci: vet and race-test every push and pull request`. When a new package appears,
add its scope to this list in the commit that introduces it.

### Rules

- **Description**: imperative mood, lowercase first letter, no trailing period,
  72 characters or fewer; name the behavior, not the file —
  `feat(loader): reconcile entries per changed field`.
- **Body**: wrap at 72 columns and explain what changed and why; reference
  specification decisions (`D8`), feature files, or issues (`Refs #12`). Add a
  body whenever the motivation is not obvious from the description.
- **Footers**: `Token: value` or `Token #value`, such as `Fixes: #12` or
  `BREAKING CHANGE: ...`.
- **Atomicity**: one logical change and one type per commit. Do not mix
  unrelated features, fixes, tests, and documentation; if the description needs
  "and", split the commit.
- **Formatting**: run `gofmt -w` on changed Go files so formatting lands in the
  functional commit, not a separate commit.

## Pull requests

- Use the same Conventional Commit form for the PR title.
- Explain the behavior and rationale, and link the relevant issue or
  specification decision (`D`-number).
- State the test results you ran (`go vet ./...`, `go test ./... -race`).
- Include example output or screenshots only when they clarify a user-visible
  change.

## Reporting bugs and requesting features

Use the [issue templates](.github/ISSUE_TEMPLATE/) — they ask for the spec
reference, a minimal reproduction, and the environment details needed to
reproduce. For behavior questions, quote the applicable section of
`specs/SPEC.md`.

## Security and configuration

Keep credentials and machine-specific configuration out of the repository. The
project has no documented environment-variable setup; if you add
configuration, describe it in the `README.md` and cover its behavior in tests.
