# Plan: Thread-Safe Topic Manager and Lookup Service Registration

## Status: IMPLEMENTED

Implementation completed. See `pkg/core/engine/engine.go` for the thread-safe implementation.

## Problem

The `Engine` struct previously had `Managers` and `LookupServices` maps that were accessed concurrently without synchronization. This could cause panics with "concurrent map read and map write" if applications dynamically add/remove topic managers while engine methods run.

## Solution

Make the maps private and provide thread-safe accessor methods. Use `Config` for initialization.

## Implementation Summary

### Engine struct (private fields):

```go
type Engine struct {
    // managers holds the registered topic managers (access via thread-safe methods)
    managers map[string]TopicManager
    // lookupServices holds the registered lookup services (access via thread-safe methods)
    lookupServices map[string]LookupService
    // ... other fields ...

    // mu protects managers and lookupServices maps for concurrent access
    mu sync.RWMutex
}
```

### Config for initialization:

```go
type Config struct {
    Managers       map[string]TopicManager
    LookupServices map[string]LookupService
    Storage        Storage
    ChainTracker   chaintracker.ChainTracker
    // ... other configuration fields ...
}

// Create a new engine:
e := engine.NewEngine(&engine.Config{
    Managers: map[string]engine.TopicManager{
        "tm_example": myTopicManager,
    },
    LookupServices: map[string]engine.LookupService{
        "ls_example": myLookupService,
    },
    // ... other config ...
})
```

### Thread-safe accessor methods:

**Topic Managers:**
- `RegisterTopicManager(name string, manager TopicManager)` - add a topic manager
- `UnregisterTopicManager(name string)` - remove a topic manager
- `GetTopicManager(name string) (TopicManager, bool)` - get a topic manager by name
- `HasTopicManager(name string) bool` - check if a topic manager exists

**Lookup Services:**
- `RegisterLookupService(name string, service LookupService)` - add a lookup service
- `UnregisterLookupService(name string)` - remove a lookup service
- `GetLookupService(name string) (LookupService, bool)` - get a lookup service by name
- `HasLookupService(name string) bool` - check if a lookup service exists

**List methods (thread-safe iteration):**
- `ListTopicManagers() map[string]*overlay.MetaData` - returns metadata for all topic managers
- `ListLookupServiceProviders() map[string]*overlay.MetaData` - returns metadata for all lookup services

### Internal helper:
- `getLookupServicesSnapshot() []LookupService` - get a snapshot of lookup services for safe iteration

### Methods using thread-safe access:

1. **`ListTopicManagers()`** - uses `RLock` for iteration
2. **`ListLookupServiceProviders()`** - uses `RLock` for iteration
3. **`Submit()`** - takes snapshot of managers under lock, uses `getLookupServicesSnapshot()` for service iteration
4. **`Lookup()`** - uses `GetLookupService()` for thread-safe access
5. **`SyncAdvertisements()`** - takes snapshot of both maps under single `RLock`
6. **`deleteUTXODeep()`** - uses `getLookupServicesSnapshot()` for service iteration
7. **`HandleNewMerkleProof()`** - uses `getLookupServicesSnapshot()` for service iteration
8. **`GetDocumentationForTopicManager()`** - uses `GetTopicManager()` for thread-safe access
9. **`GetDocumentationForLookupServiceProvider()`** - uses `GetLookupService()` for thread-safe access

### Files modified:

1. **`pkg/core/engine/engine.go`** - main implementation with private fields and Config
2. **`pkg/core/engine/gasp-storage.go`** - updated to use thread-safe accessors
3. **`pkg/core/engine/tests/*.go`** - updated tests to use `Config`
4. **`examples/custom/main.go`** - updated to use `Config`

## Design Notes

1. **RWMutex** - Use `sync.RWMutex` since reads vastly outnumber writes. Multiple goroutines can read simultaneously.

2. **Lock granularity** - Hold locks for minimal duration. Get manager/service reference under lock, then release before calling methods on it.

3. **No nested locks** - Avoid calling methods that acquire locks while holding a lock.

4. **Snapshot for iteration** - Methods like `Submit` that iterate take snapshots under lock to avoid holding lock during I/O operations.

5. **Private fields** - The `managers` and `lookupServices` fields are now private (lowercase). All access must go through the thread-safe accessor methods.

## Migration Guide

If you were previously initializing the engine directly:

```go
// Old way (no longer works):
e := &engine.Engine{
    Managers: map[string]engine.TopicManager{...},
    LookupServices: map[string]engine.LookupService{...},
}

// New way:
e := engine.NewEngine(&engine.Config{
    Managers: map[string]engine.TopicManager{...},
    LookupServices: map[string]engine.LookupService{...},
})
```

If you were accessing `Managers` or `LookupServices` directly:

```go
// Old way (no longer works):
manager := e.Managers["myTopic"]

// New way:
manager, ok := e.GetTopicManager("myTopic")
if ok {
    // use manager
}
```
