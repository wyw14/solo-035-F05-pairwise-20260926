package api

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/wyw14/solo-035/internal/service"
)

func testServer(t *testing.T) http.Handler {
	t.Helper()
	runtime, err := service.NewRuntime(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Bootstrap(runtime); err != nil {
		t.Fatal(err)
	}
	var web fs.FS = fstest.MapFS{"operations.html": &fstest.MapFile{Data: []byte("ok")}}
	return NewServer(runtime, web).Router()
}

func postControl(t *testing.T, handler http.Handler, payload string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/control", strings.NewReader(payload))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestRouterServesOperationsAPI(t *testing.T) {
	handler := testServer(t)
	request := httptest.NewRequest(http.MethodGet, "/api/operations", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var body struct {
		Operations []struct {
			Name        string `json:"name"`
			Type        string `json:"type"`
			RuleType    string `json:"rule_type"`
			RuleVersion int    `json:"rule_version"`
		} `json:"operations"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Operations) != 1 || body.Operations[0].Type != "news" {
		t.Fatalf("operations = %+v", body.Operations)
	}
	if body.Operations[0].RuleType != "news" || body.Operations[0].RuleVersion != 1 {
		t.Fatalf("rule view = %+v", body.Operations[0])
	}
}

func TestCreateOperationAcceptsEventType(t *testing.T) {
	handler := testServer(t)
	request := httptest.NewRequest(http.MethodPost, "/api/operations", strings.NewReader(`{"name":"Concert Night","type":"concert"}`))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d", recorder.Code)
	}
	var event struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "concert" {
		t.Fatalf("type = %q", event.Type)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/operations", strings.NewReader(`{"name":"Untyped"}`))
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d", recorder.Code)
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "news" {
		t.Fatalf("default type = %q", event.Type)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/operations", strings.NewReader(`{"name":"Bogus","type":"radio"}`))
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("unknown type status = %d", recorder.Code)
	}
}

func TestQualityControlActions(t *testing.T) {
	handler := testServer(t)

	recorder := postControl(t, handler, `{"action":"publish-quality-rules","event_type":"news","minimum":-60,"maximum":0,"max_age_millis":4000,"probes":[{"kind":"waveform","minimum_sources":2}]}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("publish status = %d (%s)", recorder.Code, recorder.Body.String())
	}
	var published struct {
		Value struct {
			Version int `json:"version"`
		} `json:"value"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &published); err != nil {
		t.Fatal(err)
	}
	if published.Value.Version != 2 {
		t.Fatalf("published version = %d", published.Value.Version)
	}

	recorder = postControl(t, handler, `{"action":"publish-quality-rules","event_type":"news","minimum":6,"maximum":-60,"max_age_millis":4000,"probes":[{"kind":"waveform","minimum_sources":2}]}`)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("invalid publish status = %d", recorder.Code)
	}

	recorder = postControl(t, handler, `{"action":"grant-quality-exception","event_id":"missing","item":"loudness-range","approver":"ops","message":"reason"}`)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("unknown event status = %d", recorder.Code)
	}

	recorder = postControl(t, handler, `{"action":"recalculate-permit","permit_id":"missing"}`)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("unknown permit status = %d", recorder.Code)
	}

	recorder = postControl(t, handler, `{"action":"publish-quality-rules","bogus_field":1}`)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d", recorder.Code)
	}
}

func TestQualityQueryEndpoints(t *testing.T) {
	handler := testServer(t)
	for _, path := range []string{"/api/quality/rules", "/api/quality/permits", "/api/quality/exceptions"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d", path, recorder.Code)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/api/quality/rules", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var rules struct {
		Rules []struct {
			EventType string `json:"event_type"`
			Version   int    `json:"version"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &rules); err != nil {
		t.Fatal(err)
	}
	if len(rules.Rules) != 3 {
		t.Fatalf("seeded rules = %+v", rules.Rules)
	}
}

func TestRecalculatePermitThroughAPI(t *testing.T) {
	handler := testServer(t)
	request := httptest.NewRequest(http.MethodGet, "/api/quality/permits", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var permits struct {
		Permits []struct {
			ID      string `json:"id"`
			Granted bool   `json:"granted"`
		} `json:"permits"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &permits); err != nil {
		t.Fatal(err)
	}
	if len(permits.Permits) != 1 || !permits.Permits[0].Granted {
		t.Fatalf("permits = %+v", permits.Permits)
	}
	payload, err := json.Marshal(map[string]string{"action": "recalculate-permit", "permit_id": permits.Permits[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	recorder = postControl(t, handler, string(payload))
	if recorder.Code != http.StatusOK {
		t.Fatalf("recalculate status = %d (%s)", recorder.Code, recorder.Body.String())
	}
	var audit struct {
		Value struct {
			Matches bool `json:"matches"`
		} `json:"value"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &audit); err != nil {
		t.Fatal(err)
	}
	if !audit.Value.Matches {
		t.Fatalf("audit = %+v", audit.Value)
	}
}

func TestGrantExceptionThroughAPI(t *testing.T) {
	handler := testServer(t)
	request := httptest.NewRequest(http.MethodGet, "/api/operations", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var operations struct {
		Operations []struct {
			EventID string `json:"event_id"`
		} `json:"operations"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &operations); err != nil {
		t.Fatal(err)
	}
	eventID := operations.Operations[0].EventID

	recorder = postControl(t, handler, `{"action":"grant-quality-exception","event_id":"`+eventID+`","item":"loudness-range","message":"no approver"}`)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("missing approver status = %d", recorder.Code)
	}

	recorder = postControl(t, handler, `{"action":"grant-quality-exception","event_id":"`+eventID+`","item":"loudness-range","approver":"duty-manager","message":"tone test","expires_in_millis":60000}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("grant status = %d (%s)", recorder.Code, recorder.Body.String())
	}
	var granted struct {
		Value struct {
			Rule struct {
				EventType string `json:"event_type"`
				Version   int    `json:"version"`
			} `json:"rule"`
			ExpiresAt *string `json:"expires_at"`
		} `json:"value"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &granted); err != nil {
		t.Fatal(err)
	}
	if granted.Value.Rule.EventType != "news" || granted.Value.Rule.Version != 1 {
		t.Fatalf("exception rule = %+v", granted.Value.Rule)
	}
	if granted.Value.ExpiresAt == nil {
		t.Fatal("ttl was not applied")
	}

	request = httptest.NewRequest(http.MethodGet, "/api/quality/exceptions?event_id="+eventID, nil)
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var exceptions struct {
		Exceptions []struct {
			ApprovedBy string `json:"approved_by"`
		} `json:"exceptions"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &exceptions); err != nil {
		t.Fatal(err)
	}
	if len(exceptions.Exceptions) != 1 || exceptions.Exceptions[0].ApprovedBy != "duty-manager" {
		t.Fatalf("exceptions = %+v", exceptions.Exceptions)
	}
}

func TestCheckpointRoundTripThroughAPI(t *testing.T) {
	handler := testServer(t)
	request := httptest.NewRequest(http.MethodGet, "/api/checkpoint", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("export status = %d", recorder.Code)
	}
	var checkpoint map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &checkpoint); err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{"rules", "selections", "exceptions", "permits"} {
		if _, ok := checkpoint[section]; !ok {
			t.Fatalf("checkpoint misses %s", section)
		}
	}
	encoded, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/checkpoint", bytes.NewReader(encoded))
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("restore status = %d (%s)", recorder.Code, recorder.Body.String())
	}
}
