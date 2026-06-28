# Go Coding Style Rules

## Formatting (CRITICAL)

**Always use `gofmt`**: Go's canonical formatting tool
- Run `gofmt -w .` before committing
- Use `go fmt ./...` for entire packages
- Never argue about formatting - let the machine decide

**Formatting rules:**
- Use **tabs** for indentation (gofmt default)
- No line length limit - wrap when lines feel too long
- No parentheses in control structures (`if`, `for`, `switch`)
- Opening brace on same line as declaration (never on next line)

```go
// CORRECT
if x > 0 {
    return y
}

// WRONG - brace on next line causes semicolon insertion
if x > f()
{           // wrong!
    g()
}
```

## Package Names

**Short, lowercase, single-word names:**
- No underscores or mixedCaps in package names
- Package name matches base directory name
- Keep it brief - everyone typing it will import it

```go
package buffer  // NOT buf_util or BufferUtil
package http    // NOT httpserver or HTTPServer
```

## Naming Conventions

**Exported names** (visible outside package):
- First character **uppercase** = exported
- Use `MixedCaps` for multi-word names

**Unexported names** (package-private):
- First character **lowercase** = unexported
- Use `mixedCaps` for multi-word names

**Getters/Setters:**
- Don't use `Get` prefix for getters
- Use field name directly, capitalized

```go
// WRONG
func (u *User) GetID() string { return u.id }
func (u *User) SetID(id string) { u.id = id }

// CORRECT
func (u *User) ID() string { return u.id }
func (u *User) SetID(id string) { u.id = id }
```

**Interface names:**
- One-method interfaces: method name + `-er` suffix
- `Reader`, `Writer`, `Formatter`, `CloseNotifier`
- Use canonical method names when possible

```go
type Reader interface {
    Read(p []byte) (n int, err error)
}

type Stringer interface {
    String() string
}
```

## File Organization

**MANY SMALL FILES > FEW LARGE FILES:**
- One file per type or related group
- 200-400 lines typical, 800 max
- High cohesion, low coupling
- Organize by feature/domain, not by type

**SEPARATE CONTRACT, TYPES, AND IMPLEMENTATION (one concern per file):**
- `types.go` — domain models / DTOs only (row structs, request/response)
- one file per interface — the interface + factory signatures only, **no implementation** (e.g. `store.go`, `adapter.go`, `keymanager.go`)
- impl files — concrete implementations only (e.g. `sqlite.go`, `stub.go`, `registry.go`, `*_impl.go`)
- `db.go` — resource lifecycle (`Open` / `Migrate` / connection setup)
- **Never co-locate an interface with its implementation** in the same file

Rationale: keeping the contract in its own file makes implementations swappable
and testable in isolation — an in-memory fake satisfies the interface file with
zero coupling to the real (e.g. sqlite) implementation.

```go
// store/types.go        — DTO only
type Profile struct { Name string; ... }

// store/store.go        — interface only
type ProfileStore interface { GetProfile(ctx, name) (*Profile, error) }

// store/sqlite.go       — implementation only
type sqliteProfileStore struct{ db *sql.DB }
```

## Error Handling (CRITICAL)

**ALWAYS handle errors explicitly:**
- Functions return `(result, error)` pairs
- Never ignore error returns
- Check errors immediately after calls
- Use "comma ok" idiom for type assertions

```go
// CORRECT
file, err := os.Open("config.txt")
if err != nil {
    log.Fatalf("failed to open config: %v", err)
}
defer file.Close()

// WRONG - ignoring error
file, _ := os.Open("config.txt")
```

**Error return pattern:**
- Return early on errors
- Success path runs down the page
- No unnecessary `else` after error return

```go
// CORRECT
func processFile(name string) error {
    f, err := os.Open(name)
    if err != nil {
        return err
    }
    defer f.Close()

    d, err := f.Stat()
    if err != nil {
        return err
    }

    process(f, d)
    return nil
}
```

## Control Structures

**No parentheses in control structures:**
```go
// CORRECT
if x > 0 {
    return y
}

for i := 0; i < 10; i++ {
    sum += i
}

// WRONG
if (x > 0) {
    return y
}
```

**Range loops for collections:**
```go
// Over slice/array/map
for key, value := range myMap {
    fmt.Printf("%s: %v\n", key, value)
}

// Only keys
for key := range myMap {
    if key.expired() {
        delete(myMap, key)
    }
}

// Only values (use blank identifier)
for _, value := range mySlice {
    sum += value
}
```

**Type switch:**
```go
switch v := value.(type) {
case int:
    fmt.Printf("integer %d\n", v)
case string:
    fmt.Printf("string %s\n", v)
default:
    fmt.Printf("unknown type %T\n", v)
}
```

## Functions

**Multiple return values:**
```go
func nextInt(b []byte, pos int) (value, nextPos int) {
    // ... implementation
    return value, nextPos
}
```

**Named result parameters:**
```go
func ReadFull(r Reader, buf []byte) (n int, err error) {
    for len(buf) > 0 && err == nil {
        var nr int
        nr, err = r.Read(buf)
        n += nr
        buf = buf[nr:]
    }
    return  // uses current values of n and err
}
```

**Defer for cleanup:**
```go
func processFile(filename string) (string, error) {
    f, err := os.Open(filename)
    if err != nil {
        return "", err
    }
    defer f.Close()  // runs when function returns

    // ... process file
    return result, nil
}
```

## Data Structures

**Use `make` for slices, maps, channels:**
```go
// CORRECT
slice := make([]int, 10)
m := make(map[string]int)
ch := make(chan int, 100)

// WRONG - new returns pointer to zero value
var p *[]int = new([]int)  // *p == nil; rarely useful
```

**Slices over arrays:**
- Prefer slices in most cases
- Arrays are values (copied on assignment)
- Slices are references to underlying arrays

```go
// Array (value type)
arr := [3]int{1, 2, 3}

// Slice (reference type)
slice := []int{1, 2, 3}
slice = append(slice, 4)
```

**Map with comma ok idiom:**
```go
value, ok := myMap[key]
if !ok {
    // key not present
}
```

## Concurrency

**Goroutines for concurrent work:**
```go
go func() {
    // runs concurrently
    process(data)
}()
```

**Channels for communication:**
```go
ch := make(chan int)
go func() {
    ch <- calculate()  // send result
}()
result := <-ch  // wait for result
```

**Share memory by communicating:**
- Pass data through channels
- Avoid explicit mutexes when possible
- One goroutine owns the data at a time

## Methods

**Pointers vs Values:**
- Use pointer receivers when method needs to mutate
- Use value receivers for immutable operations
- Value methods can be called on pointers automatically

```go
type Counter struct {
    count int
}

// Pointer receiver - mutates
func (c *Counter) Increment() {
    c.count++
}

// Value receiver - doesn't mutate
func (c Counter) Count() int {
    return c.count
}
```

## Code Quality Checklist

Before marking work complete:
- [ ] Code is formatted with `gofmt`
- [ ] Package name is lowercase, single word
- [ ] Exported names use `MixedCaps`
- [ ] All errors are handled (never ignored)
- [ ] Functions are small (<50 lines)
- [ ] Files are focused (<800 lines)
- [ ] Interface, DTO (`types.go`), and implementation live in separate files
- [ ] No deep nesting (>4 levels)
- [ ] Defer used for cleanup
- [ ] Named return parameters clarify intent
- [ ] Interfaces use `-er` suffix for single-method interfaces
- [ ] Comments are complete for exported declarations
- [ ] No unnecessary comments (code is self-documenting)

## Documentation

**Exported declarations need doc comments:**
```go
// ReadAll reads from r until an error or EOF and returns the data it read.
// A successful call returns err == nil, not err == EOF.
// Because ReadAll is defined to read from src until EOF,
// it does not treat an EOF from Read as an error to be reported.
func ReadAll(r io.Reader) ([]byte, error) {
    // ...
}
```

**Doc comment conventions:**
- Comment starts with name of exported item
- Complete sentences
- No blank line between comment and declaration
- Focus on what, not how (for exported APIs)

## Interfaces

**Implicit implementation:**
- Types implement interfaces without explicit declaration
- Small interfaces (1-2 methods) preferred
- Define interfaces where they're used

```go
// Define interface where it's consumed
func processData(r io.Reader) error {
    // ... any type implementing io.Reader works
}
```

## Blank Identifier

**Use `_` for ignored values:**
```go
// Ignore index, use value
for _, value := range slice {
    fmt.Println(value)
}

// Ignore error when it's safe (document why!)
data, _ := os.ReadFile("config.json")
```

## Custom Project Rules

### Database/Store Package Organization

**Package structure for data access layers:**
- `types.go` — Domain models/DTOs only (structs, no logic)
- `interfaces.go` — Interface definitions only (no implementations)
- `db.go` — Database lifecycle (Open, Migrate, connection setup)
- Implementation subdirectory (e.g., `sqlite/`) — All database-specific code

**Implementation subdirectory organization:**
- One file per table/domain entity (e.g., `profile.go`, `secret.go`, `kv.go`)
- Each file contains the store implementation for one entity
- Shared helper functions in `db.go` or separate `util.go`
- Maximum 100-150 lines per file

**Test organization:**
- Test files MUST match implementation file names
  - `profile.go` → `profile_test.go`
  - `secret.go` → `secret_test.go`
  - `kv.go` → `kv_test.go`
  - `db.go` → `db_test.go`
- Shared test utilities in `testutil.go`
- Tests reside in the same subdirectory as implementations

```go
// internal/store/
├── types.go           // Domain models
├── interfaces.go      // Interface definitions
├── sqlite/            // SQLite implementation
│   ├── db.go         // DB lifecycle, migrations, helpers
│   ├── profile.go    // ProfileStore implementation
│   ├── secret.go     // SecretStore implementation
│   ├── kv.go         // KVStore implementation
│   ├── testutil.go   // Shared test helpers
│   ├── db_test.go
│   ├── profile_test.go
│   ├── secret_test.go
│   └── kv_test.go
```

### Error Handling Conventions

**Error wrapping:**
- Use `fmt.Errorf` with `%w` verb for error wrapping
- Preserve error chain for `errors.Is` and `errors.As`
- Add context to errors before returning

```go
// CORRECT
if err := db.Exec(query); err != nil {
    return fmt.Errorf("failed to create profile: %w", err)
}

// WRONG - loses error context
if err := db.Exec(query); err != nil {
    return err
}
```

**Return early on errors:**
- Success path runs down the page
- No unnecessary `else` after error return
- Always check errors immediately

### Context Propagation

**Context usage:**
- Always pass `context.Context` as first parameter
- Use `context.Background()` for tests or CLI entry points
- Use `ctx.Value()` for request-scoped data when needed
- Respect context cancellation in long-running operations
- If a parameter is required by convention but unused in the body (e.g. `context.Context` kept first), name it `_` — never leave an unused named parameter

```go
// CORRECT - ctx slot kept by convention, but unused in the body
func Setup(_ context.Context, cfg Config) error {
    return nil
}

// WRONG - unused named parameter trips linters and signals nothing
func Setup(ctx context.Context, cfg Config) error {
    return nil
}
```

```go
// CORRECT
func (s *Store) AddProfile(ctx context.Context, p *Profile) error {
    // Use ctx in all database calls
    _, err := s.db.ExecContext(ctx, query, args...)
    if err != nil {
        return fmt.Errorf("add profile: %w", err)
    }
    return nil
}

// WRONG - no context
func (s *Store) AddProfile(p *Profile) error {
    _, err := s.db.Exec(query, args...)
    return err
}
```

### Database Operations

**Query patterns:**
- Use `ExecContext` for INSERT, UPDATE, DELETE
- Use `QueryRowContext` for single-row SELECT
- Use `QueryContext` for multi-row SELECT
- Always defer `rows.Close()` for `QueryContext`
- Check `rows.Err()` after iteration

```go
// CORRECT
rows, err := s.db.QueryContext(ctx, "SELECT name FROM profiles")
if err != nil {
    return err
}
defer rows.Close()

var profiles []string
for rows.Next() {
    var name string
    if err := rows.Scan(&name); err != nil {
        return err
    }
    profiles = append(profiles, name)
}
return rows.Err()  // Check iteration errors
```

**Validation:**
- Validate inputs before database operations
- Return descriptive errors for validation failures
- Use meaningful error messages

### Testing Conventions

**Test setup:**
- Use `setupTestDB(t *testing.T)` helper for consistent test database creation
- Always cleanup temporary resources in `defer`
- Use `t.Fatal` for setup failures, `t.Error` for test failures

**Test naming:**
- `TestFunctionName` for unit tests
- `TestFunctionName_Condition` for specific scenarios
- Use descriptive test names that explain what is being tested

```go
// CORRECT
func TestProfileStore_AddDuplicateProfile_ReturnsError(t *testing.T) {
    db, cleanup := setupTestDB(t)
    defer cleanup()
    
    // ... test implementation
}
```

### Code Organization

**Import grouping:**
- Standard library imports first
- Third-party imports second
- Internal imports last
- Separate groups with blank lines

```go
// CORRECT
import (
    "context"
    "database/sql"
    "fmt"
    
    "github.com/lib/pq"
    
    "github.com/oniharnantyo/onclaw/internal/config"
)
```
