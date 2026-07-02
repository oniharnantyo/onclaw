package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestWebSkillsUpload(t *testing.T) {
	db, dbCleanup := setupTestDB(t)
	defer dbCleanup()

	// Seed web password
	hash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt hash failed: %v", err)
	}
	_, err = db.Exec("INSERT OR REPLACE INTO preferences (key, value) VALUES ('web_password_hash', ?)", string(hash))
	if err != nil {
		t.Fatalf("seed password failed: %v", err)
	}

	_, addr, srvCleanup := setupTestServer(t, db, nil)
	defer srvCleanup()

	// Login to get session cookie
	client := &http.Client{}
	loginBody, _ := json.Marshal(map[string]string{"password": "secret123"})
	resp, err := client.Post("http://"+addr+"/api/login", "application/json", bytes.NewReader(loginBody))
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login failed: %s", resp.Status)
	}
	cookies := resp.Cookies()

	// Create zip file in memory
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)

	// Create SKILL.md inside zip
	fw, err := zw.Create("SKILL.md")
	if err != nil {
		t.Fatalf("failed to create SKILL.md in zip: %v", err)
	}
	metadata := `---
name: test-skill
description: "A test upload skill"
---
Test body`
	_, _ = fw.Write([]byte(metadata))
	zw.Close()

	// Prepare multipart form upload
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)
	part, err := writer.CreateFormFile("file", "test-skill.zip")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	_, _ = io.Copy(part, &zipBuf)
	writer.Close()

	// Send POST /api/skills/upload request
	req, err := http.NewRequest("POST", "http://"+addr+"/api/skills/upload", &requestBody)
	if err != nil {
		t.Fatalf("failed to create upload request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Origin", "http://"+addr)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}

	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("upload request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d. Body: %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse upload result
	var result struct {
		Source      string `json:"source"`
		PackageName string `json:"package_name"`
		Skills      []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"skills"`
	}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		t.Fatalf("failed to decode upload result: %v", err)
	}

	if result.PackageName != "test-skill" {
		t.Errorf("expected package name 'test-skill', got '%s'", result.PackageName)
	}
	if len(result.Skills) != 1 || result.Skills[0].Name != "test-skill" {
		t.Errorf("expected 1 skill named 'test-skill', got: %+v", result.Skills)
	}
	if result.Source == "" {
		t.Errorf("expected temp file source to be returned, got empty")
	}
}
