package file

import "sync"

// All file repositories in one process share this lock. Workspace deletion
// touches several JSON files and must not race a write from another repo.
var dataMu sync.Mutex
