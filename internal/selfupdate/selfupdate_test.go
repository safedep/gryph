package selfupdate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsNewer(t *testing.T) {
	tests := []struct {
		name     string
		current  string
		latest   string
		expected bool
	}{
		{"newer version available", "v1.0.0", "v1.1.0", true},
		{"same version", "v1.0.0", "v1.0.0", false},
		{"older version on remote", "v1.1.0", "v1.0.0", false},
		{"no v prefix current", "1.0.0", "v1.1.0", true},
		{"no v prefix latest", "v1.0.0", "1.1.0", true},
		{"no v prefix both", "1.0.0", "1.1.0", true},
		{"devel version", "(devel)", "v1.0.0", false},
		{"invalid current", "invalid", "v1.0.0", false},
		{"invalid latest", "v1.0.0", "invalid", false},
		{"patch version newer", "v1.0.0", "v1.0.1", true},
		{"major version newer", "v1.0.0", "v2.0.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isNewer(tt.current, tt.latest))
		})
	}
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		body           string
		currentVersion string
		wantErr        bool
		wantAvailable  bool
	}{
		{
			name:       "newer version available",
			statusCode: http.StatusOK,
			body: `{
				"tag_name": "v1.1.0",
				"html_url": "https://github.com/safedep/gryph/releases/tag/v1.1.0"
			}`,
			currentVersion: "v1.0.0",
			wantAvailable:  true,
		},
		{
			name:       "already on latest",
			statusCode: http.StatusOK,
			body: `{
				"tag_name": "v1.0.0",
				"html_url": "https://github.com/safedep/gryph/releases/tag/v1.0.0"
			}`,
			currentVersion: "v1.0.0",
			wantAvailable:  false,
		},
		{
			name:           "non-200 response",
			statusCode:     http.StatusNotFound,
			body:           `{"message": "Not Found"}`,
			currentVersion: "v1.0.0",
			wantErr:        true,
		},
		{
			name:           "malformed JSON",
			statusCode:     http.StatusOK,
			body:           `{malformed`,
			currentVersion: "v1.0.0",
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			checker := NewChecker(WithBaseURL(server.URL))
			result, err := checker.Check(context.Background(), &CheckInput{Version: tt.currentVersion})

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantAvailable, result.UpdateAvailable)
			assert.Equal(t, tt.currentVersion, result.CurrentVersion)
		})
	}
}

func TestCheckTrailingSlashBaseURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/safedep/gryph/releases/latest", r.URL.Path)
		_, _ = w.Write([]byte(`{"tag_name": "v1.1.0", "html_url": "https://example.com/v1.1.0"}`))
	}))
	defer server.Close()

	checker := NewChecker(WithBaseURL(server.URL + "/"))
	result, err := checker.Check(context.Background(), &CheckInput{Version: "v1.0.0"})
	require.NoError(t, err)
	assert.True(t, result.UpdateAvailable)
}

func TestCheckAsync(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name": "v2.0.0", "html_url": "https://example.com/v2.0.0"}`))
	}))
	defer server.Close()

	checker := NewChecker(WithBaseURL(server.URL))
	ch := checker.CheckAsync(context.Background(), &CheckInput{Version: "v1.0.0"})

	select {
	case result, ok := <-ch:
		require.True(t, ok)
		assert.True(t, result.UpdateAvailable)
		assert.Equal(t, "v2.0.0", result.LatestVersion)
	case <-time.After(5 * time.Second):
		t.Fatal("CheckAsync did not return in time")
	}
}

func TestCheckAsyncError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	checker := NewChecker(WithBaseURL(server.URL))
	ch := checker.CheckAsync(context.Background(), &CheckInput{Version: "v1.0.0"})

	select {
	case _, ok := <-ch:
		assert.False(t, ok, "channel should be closed without sending on error")
	case <-time.After(5 * time.Second):
		t.Fatal("CheckAsync did not return in time")
	}
}
