# Database Auto-Discovery Design

## Problem

Current default `--db` points to `~/.vc/vc.db` (global database), which causes:
- Database location doesn't match working directory
- Workers spawn in one project but read issues from another
- Can't run VC on multiple projects simultaneously
- Dangerous mismatch between issue context and code context

## Solution: Git-Like Auto-Discovery

### 1. Auto-Discovery Algorithm

```go
func discoverDatabase() (string, error) {
    // Start from current directory
    dir, _ := os.Getwd()

    // Walk up directory tree
    for {
        // Check for .beads/*.db
        beadsDir := filepath.Join(dir, ".beads")
        if entries, err := os.ReadDir(beadsDir); err == nil {
            for _, entry := range entries {
                if filepath.Ext(entry.Name()) == ".db" {
                    return filepath.Join(beadsDir, entry.Name()), nil
                }
            }
        }

        // Move to parent directory
        parent := filepath.Dir(dir)
        if parent == dir {
            // Reached filesystem root
            break
        }
        dir = parent
    }

    return "", fmt.Errorf("no .beads/*.db found in current directory or parents")
}
```

### 2. Working Directory Alignment

The executor's `WorkingDir` MUST be the directory containing `.beads/`:

```go
func getProjectRoot(dbPath string) string {
    // If dbPath is /foo/bar/.beads/project.db
    // Return /foo/bar
    beadsDir := filepath.Dir(dbPath)  // /foo/bar/.beads
    return filepath.Dir(beadsDir)     // /foo/bar
}
```

### 3. Validation

Before executing, validate alignment:

```go
func validateConfiguration(dbPath, workingDir string) error {
    projectRoot := getProjectRoot(dbPath)
    absWorkingDir, _ := filepath.Abs(workingDir)

    if projectRoot != absWorkingDir {
        return fmt.Errorf(
            "database-working directory mismatch:\n"+
            "  database: %s (project: %s)\n"+
            "  working dir: %s\n"+
            "  These must match! Database and code must be in same project.",
            dbPath, projectRoot, absWorkingDir)
    }
    return nil
}
```

## Usage Patterns

### Pattern 1: Local Project (Typical)

```bash
cd ~/myproject
vc execute                # Auto-discovers .beads/project.db
                          # WorkingDir: ~/myproject
                          # Database: ~/myproject/.beads/project.db
                          # ✓ Aligned
```

### Pattern 2: Explicit Database (Advanced)

```bash
cd ~/myproject
vc execute --db ~/otherproject/.beads/db.db
# ERROR: database-working directory mismatch
# Either:
#   cd ~/otherproject && vc execute
# Or:
#   vc execute --db .beads/project.db
```

### Pattern 3: VC Working on Itself

```bash
cd ~/src/vc/vc
vc execute                # Auto-discovers .beads/vc.db
                          # WorkingDir: ~/src/vc/vc
                          # Database: ~/src/vc/vc/.beads/vc.db
                          # ✓ Aligned - VC improves itself
```

## Init Command

```bash
vc init [project-name]

# Creates:
#   .beads/
#   .beads/<project-name>.db (or auto-generated name)
#   .beads/issues.jsonl
#   .gitignore (add .beads/*.db if git present)
```

## Migration Path

1. **Phase 1**: Add auto-discovery, keep `~/.vc/vc.db` as fallback with warning
2. **Phase 2**: Make auto-discovery required, error if not found (suggest `vc init`)
3. **Phase 3**: Remove global database support entirely

## Benefits

- **No more confusion**: Database always matches codebase
- **Multiple projects**: Can run VC on different projects simultaneously
- **Intuitive**: Works like git, no surprise
s
- **Safe**: Can't accidentally modify wrong project
- **Isolated**: Each project has its own issue tracker

## Implementation Checklist

- [ ] Add `discoverDatabase()` function
- [ ] Add `getProjectRoot()` function
- [ ] Add `validateConfiguration()` function
- [ ] Modify `main.go` to use auto-discovery
- [ ] Add `vc init` command
- [ ] Update executor to validate alignment
- [ ] Add tests for discovery algorithm
- [ ] Update documentation
