package api

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/wyw14/solo-035/internal/service"
)

func TestRouterServesOperationsAPI(t *testing.T) {
	runtime, err := service.NewRuntime(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Bootstrap(runtime); err != nil {
		t.Fatal(err)
	}
	var web fs.FS = fstest.MapFS{"operations.html": &fstest.MapFile{Data: []byte("ok")}}
	request := httptest.NewRequest(http.MethodGet, "/api/operations", nil)
	recorder := httptest.NewRecorder()
	NewServer(runtime, web).Router().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestQualityRuleLifecycleAPI(t *testing.T) {
	runtime, err := service.NewRuntime(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Bootstrap(runtime); err != nil {
		t.Fatal(err)
	}
	var web fs.FS = fstest.MapFS{"operations.html": &fstest.MapFile{Data: []byte("ok")}}
	server := NewServer(runtime, web).Router()

	payload := []byte(`{
		"action": "publish-quality-rule",
		"profile": "music",
		"minimum_loudness": -20,
		"maximum_loudness": 1,
		"maximum_age_millis": 2500,
		"required_kinds": ["waveform", "integrated-loudness"],
		"minimum_sources": 2
	}`)
	request := httptest.NewRequest(http.MethodPost, "/api/control", bytes.NewReader(payload))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("publish status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var result struct {
		Value struct {
			ID      string `json:"id"`
			Version int    `json:"version"`
		} `json:"value"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Value.ID != "rule-music-v2" || result.Value.Version != 2 {
		t.Fatalf("published rule = %+v", result.Value)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/quality", nil)
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("quality status = %d", recorder.Code)
	}
	var view struct {
		Rules []struct {
			ID string `json:"id"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, rule := range view.Rules {
		if rule.ID == "rule-music-v2" {
			found = true
		}
	}
	if !found {
		t.Fatal("published rule missing from quality catalog")
	}
}

func TestCreateOperationAcceptsProfile(t *testing.T) {
	runtime, err := service.NewRuntime(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var web fs.FS = fstest.MapFS{"operations.html": &fstest.MapFile{Data: []byte("ok")}}
	server := NewServer(runtime, web).Router()

	request := httptest.NewRequest(http.MethodPost, "/api/operations",
		bytes.NewReader([]byte(`{"name":"Match Night","profile":"sports"}`)))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var event struct {
		Profile string `json:"profile"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event.Profile != "sports" {
		t.Fatalf("profile = %s", event.Profile)
	}
}
