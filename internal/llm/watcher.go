package llm

import (
	"context"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

// StartDBWatcher starts a background goroutine that watches the database directory.
// When there are write events to the db or db-wal files, it triggers a reload on the Service.
func StartDBWatcher(ctx context.Context, dbPath string, s *Service) (*fsnotify.Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	dir := filepath.Dir(dbPath)
	dbName := filepath.Base(dbPath)
	walName := dbName + "-wal"

	if err := watcher.Add(dir); err != nil {
		watcher.Close()
		return nil, err
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				watcher.Close()
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
					name := filepath.Base(event.Name)
					if name == dbName || name == walName {
						s.TriggerReload()
					}
				}
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()

	return watcher, nil
}
