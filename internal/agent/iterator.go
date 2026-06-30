package agent

import "github.com/cloudwego/eino/schema"

// EventIterator defines the iterator returned by Run.
type EventIterator interface {
	Next() (*schema.AgenticMessage, bool)
	Err() error
}
