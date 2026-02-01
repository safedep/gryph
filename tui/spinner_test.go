package tui

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRunWithSpinner(t *testing.T) {
	tests := []struct {
		name      string
		fn        func() (string, error)
		wantVal   string
		wantErr   error
		wantEmpty bool
	}{
		{
			name:      "returns value from fn",
			fn:        func() (string, error) { return "hello", nil },
			wantVal:   "hello",
			wantEmpty: true,
		},
		{
			name:      "propagates error from fn",
			fn:        func() (string, error) { return "", errors.New("fail") },
			wantErr:   errors.New("fail"),
			wantEmpty: true,
		},
		{
			name:      "returns value even when fn also returns error",
			fn:        func() (string, error) { return "partial", errors.New("warn") },
			wantVal:   "partial",
			wantErr:   errors.New("warn"),
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			got, err := RunWithSpinner("testing...", tt.fn, WithWriter(&buf))

			assert.Equal(t, tt.wantVal, got)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}
			if tt.wantEmpty {
				assert.Empty(t, buf.String(), "non-TTY writer should produce no spinner output")
			}
		})
	}
}
