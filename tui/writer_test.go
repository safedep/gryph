package tui

import (
	"bytes"
	"errors"
	"testing"
)

type failWriter struct {
	failAfter int
	written   int
}

func (fw *failWriter) Write(p []byte) (int, error) {
	if fw.written >= fw.failAfter {
		return 0, errors.New("write failed")
	}
	fw.written += len(p)
	return len(p), nil
}

func TestTableWriter_NoError(t *testing.T) {
	var buf bytes.Buffer
	tw := &tableWriter{w: &buf}

	tw.printf("hello %s\n", "world")
	tw.println("line two")

	if err := tw.Err(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	want := "hello world\nline two\n"
	if got := buf.String(); got != want {
		t.Fatalf("output mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestTableWriter_CapturesFirstError(t *testing.T) {
	fw := &failWriter{failAfter: 0}
	tw := &tableWriter{w: fw}

	tw.printf("this will fail")
	if tw.Err() == nil {
		t.Fatal("expected error after write to failing writer")
	}
}

func TestTableWriter_SkipsWritesAfterError(t *testing.T) {
	fw := &failWriter{failAfter: 0}
	tw := &tableWriter{w: fw}

	tw.printf("first")
	firstErr := tw.Err()

	tw.printf("second")
	tw.println("third")

	if tw.Err() != firstErr {
		t.Fatal("expected error to remain the same after skipped writes")
	}
}
