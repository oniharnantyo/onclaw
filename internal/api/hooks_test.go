package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/oniharnantyo/onclaw/internal/store"
	"github.com/oniharnantyo/onclaw/internal/store/sqlite"
	"golang.org/x/crypto/bcrypt"
)

func TestHooksAPI(t *testing.T) {
	db, cleanupDB := setupTestDB(t)
	defer cleanupDB()

	// Seed web password hash
	hash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}
	kv := sqlite.NewKVStore(db)
	if err := kv.Set(context.Background(), "web_password_hash", string(hash)); err != nil {
		t.Fatalf("failed to set preference: %v", err)
	}

	_, addr, cleanupServer := setupTestServer(t, db, nil)
	defer cleanupServer()

	client := newTestClient()

	// Login
	loginURL := fmt.Sprintf("http://%s/api/login", addr)
	loginBody, _ := json.Marshal(map[string]string{"password": "secret123"})
	resp, err := client.Post(loginURL, "application/json", bytes.NewReader(loginBody))
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status expected 200, got %d", resp.StatusCode)
	}

	// 1. Create a hook
	hook := &store.Hook{
		ID:          "api-hook-1",
		Name:        "api-test-hook",
		Scope:       "global",
		Event:       "pre_tool_use",
		HandlerType: "command",
		Config:      `{"command":"exit 0"}`,
		Matcher:     "exec",
		TimeoutMS:   5000,
		OnTimeout:   "block",
		Priority:    10,
	}
	createBody, _ := json.Marshal(hook)
	hooksURL := fmt.Sprintf("http://%s/api/hooks", addr)
	resp, err = client.Post(hooksURL, "application/json", bytes.NewReader(createBody))
	if err != nil {
		t.Fatalf("create hook failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("create hook expected 201 Created, got %d", resp.StatusCode)
	}

	// 2. List hooks
	resp, err = client.Get(hooksURL)
	if err != nil {
		t.Fatalf("list hooks failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("list hooks expected 200 OK, got %d", resp.StatusCode)
	}
	var hooksList []*store.Hook
	if err := json.NewDecoder(resp.Body).Decode(&hooksList); err != nil {
		t.Fatalf("failed to decode hooks list: %v", err)
	}
	if len(hooksList) != 1 || hooksList[0].Name != "api-test-hook" {
		t.Errorf("unexpected list result: %+v", hooksList)
	}

	// 3. Get single hook
	singleURL := fmt.Sprintf("http://%s/api/hooks/%s", addr, hook.ID)
	resp, err = client.Get(singleURL)
	if err != nil {
		t.Fatalf("get hook failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("get hook expected 200 OK, got %d", resp.StatusCode)
	}

	// 4. Update hook
	hook.Priority = 20
	updateBody, _ := json.Marshal(hook)
	req, _ := http.NewRequest(http.MethodPut, singleURL, bytes.NewReader(updateBody))
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("update hook failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("update hook expected 200 OK, got %d", resp.StatusCode)
	}

	// 5. Toggle hook
	toggleURL := fmt.Sprintf("http://%s/api/hooks/%s/toggle", addr, hook.ID)
	toggleBody, _ := json.Marshal(map[string]bool{"enabled": false})
	resp, err = client.Post(toggleURL, "application/json", bytes.NewReader(toggleBody))
	if err != nil {
		t.Fatalf("toggle hook failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("toggle hook expected 200 OK, got %d", resp.StatusCode)
	}

	// 6. Test dry run
	testURL := fmt.Sprintf("http://%s/api/hooks/test", addr)
	testBody, _ := json.Marshal(hook)
	resp, err = client.Post(testURL, "application/json", bytes.NewReader(testBody))
	if err != nil {
		t.Fatalf("test hook failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("test hook expected 200 OK, got %d", resp.StatusCode)
	}

	// 7. Delete hook
	req, _ = http.NewRequest(http.MethodDelete, singleURL, nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("delete hook failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("delete hook expected 204 No Content, got %d", resp.StatusCode)
	}
}
