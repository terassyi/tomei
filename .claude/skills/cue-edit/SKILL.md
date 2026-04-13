---
name: cue-edit
description: Post-CUE-edit workflow — vendor presets into examples, validate, format-check. Run after editing cuemodule/ files.
allowed-tools:
  - Bash(make vendor-cue)
  - Bash(make unvendor-cue)
  - Bash(make lint)
  - Bash(cue fmt *)
  - Bash(cue vet *)
  - Bash(bin/tomei validate *)
  - Bash(bin/tomei plan *)
  - Bash(git diff *)
  - Bash(git status*)
argument-hint: "[--clean] — pass --clean to unvendor first"
---

# CUE Edit Post-Processing

After editing files under `cuemodule/` (schema or presets), run the vendor → validate → format pipeline.

## Dynamic context

```!
git diff --name-only -- 'cuemodule/'
```

## Steps

1. If `--clean` argument: `make unvendor-cue` first.

2. **Vendor**: `make vendor-cue`

3. **Format check**:
   ```
   cue fmt --check ./cuemodule/...
   ```
   If issues found: `cue fmt ./cuemodule/...`

4. **Validate CUE module**: `cd cuemodule && cue vet ./...`

5. **Validate examples**: `bin/tomei validate examples/minimal`

6. **Show vendored diff**: `git diff --stat -- 'examples/'`

## Important

- `examples/*/cue.mod/pkg/` is generated — NEVER edit those files directly
- NEVER run `tomei apply` or `tomei init`
- NEVER commit unless user explicitly asks
