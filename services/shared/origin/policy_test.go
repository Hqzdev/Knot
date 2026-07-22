package origin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPolicyAllowsOnlyConfiguredBrowserOrigins(t *testing.T) {
	policy, err := New("https://app.example,http://localhost:5173", nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := policy.Wrap(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusAccepted)
	}))

	allowed := httptest.NewRequest(http.MethodGet, "/v1/resource", nil)
	allowed.Header.Set("Origin", "https://app.example")
	allowedResponse := httptest.NewRecorder()
	handler.ServeHTTP(allowedResponse, allowed)
	if allowedResponse.Code != http.StatusAccepted || allowedResponse.Header().Get("Access-Control-Allow-Origin") != "https://app.example" {
		t.Fatalf("unexpected allowed response: %d %#v", allowedResponse.Code, allowedResponse.Header())
	}

	denied := httptest.NewRequest(http.MethodGet, "/v1/resource", nil)
	denied.Header.Set("Origin", "https://attacker.example")
	deniedResponse := httptest.NewRecorder()
	handler.ServeHTTP(deniedResponse, denied)
	if deniedResponse.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden response, got %d", deniedResponse.Code)
	}

	native := httptest.NewRequest(http.MethodGet, "/v1/resource", nil)
	if !policy.Allows(native) {
		t.Fatal("expected native request without Origin to be allowed")
	}
}

func TestPolicyHandlesPreflightWithoutCallingApplication(t *testing.T) {
	policy, err := New("", []string{"http://localhost:5173"})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	handler := policy.Wrap(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		called = true
	}))
	request := httptest.NewRequest(http.MethodOptions, "/v1/resource", nil)
	request.Header.Set("Origin", "http://localhost:5173")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || called {
		t.Fatalf("unexpected preflight result: %d %t", response.Code, called)
	}
}

func TestPolicyRejectsUnsafeConfiguration(t *testing.T) {
	for _, value := range []string{"*", "file:///tmp/app", "https://app.example/path", "https://user@app.example", "https://app.example?value=1"} {
		if _, err := New(value, nil); err == nil {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
}
