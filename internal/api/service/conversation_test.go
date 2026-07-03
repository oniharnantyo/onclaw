package service_test

import (
	"context"
	"testing"
)

func TestService_ListConversations(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	list, err := f.svc.ListConversations(ctx)
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d", len(list))
	}
}

func TestService_ListMessages(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	list, err := f.svc.ListMessages(ctx, 123)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d", len(list))
	}
}
