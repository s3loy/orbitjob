package apperror

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestStatusForCode(t *testing.T) {
	tests := []struct {
		code     Code
		expected int
	}{
		{CodeMalformedRequest, http.StatusBadRequest},
		{CodeValidation, http.StatusBadRequest},
		{CodeUnauthorized, http.StatusUnauthorized},
		{CodeForbidden, http.StatusForbidden},
		{CodeNotFound, http.StatusNotFound},
		{CodeConflict, http.StatusConflict},
		{CodeRateLimited, http.StatusTooManyRequests},
		{CodeQuotaExhausted, http.StatusTooManyRequests},
		{CodeInternal, http.StatusInternalServerError},
		{CodeServiceUnavailable, http.StatusServiceUnavailable},
		{"unknown", http.StatusInternalServerError},
	}

	for _, tc := range tests {
		got := StatusForCode(tc.code)
		if got != tc.expected {
			t.Errorf("StatusForCode(%q) = %d, want %d", tc.code, got, tc.expected)
		}
	}
}

func TestWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	Write(c, http.StatusBadRequest, APIError{
		Code:    CodeValidation,
		Message: "name is required",
		Field:   "name",
	})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	var body ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}
	if body.Error.Code != CodeValidation {
		t.Errorf("expected code %q, got %q", CodeValidation, body.Error.Code)
	}
	if body.Error.Message != "name is required" {
		t.Errorf("expected message %q, got %q", "name is required", body.Error.Message)
	}
	if body.Error.Field != "name" {
		t.Errorf("expected field %q, got %q", "name", body.Error.Field)
	}
}

func TestWrite_RateLimitedSetsRetryAfter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	Write(c, http.StatusTooManyRequests, APIError{
		Code:    CodeRateLimited,
		Message: "rate limit exceeded",
	})

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status %d, got %d", http.StatusTooManyRequests, w.Code)
	}
	if got := w.Header().Get("Retry-After"); got != "1" {
		t.Errorf("expected Retry-After=1, got %q", got)
	}
}
