package ocr

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLanguages(t *testing.T) {
	list := "List of available languages in \"/usr/share/tesseract-ocr/5/tessdata/\" (3):\neng\nfra\nosd\n"
	if got := parseLanguages(list); !slices.Equal(got, []string{"eng", "fra"}) {
		t.Fatalf("parse: %v", got)
	}
	if got := pickLanguages([]string{"deu", "eng", "fra"}); !slices.Equal(got, []string{"fra", "eng"}) {
		t.Fatalf("pick: %v", got)
	}
	if got := pickLanguages([]string{"deu"}); !slices.Equal(got, []string{"deu"}) {
		t.Fatalf("pick without the preferred ones: %v", got)
	}
	if got := pickLanguages(nil); len(got) != 0 {
		t.Fatalf("pick with none: %v", got)
	}
}

func TestCleanup(t *testing.T) {
	in := "Bonjour  \n\n\n\nle monde\t\n\f"
	if got := cleanup(in); got != "Bonjour\n\nle monde" {
		t.Fatalf("got %q", got)
	}
}

// fakeTesseract puts a tesseract in PATH that answers langs to
// --list-langs and text to anything else.
func fakeTesseract(t *testing.T, langs, text string) {
	dir := t.TempDir()
	for name, content := range map[string]string{"langs": langs, "text": text} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = --list-langs ]; then cat %[1]s/langs; else cat %[1]s/text; fi\n", dir)
	if err := os.WriteFile(filepath.Join(dir, "tesseract"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/usr/bin:/bin")
}

func TestText(t *testing.T) {
	fakeTesseract(t, "List of available languages (2):\nfra\nosd\n", "Bonjour  \n\n\n\nle monde\n\f")
	got, err := Text(context.Background(), "capture.png")
	if err != nil || got != "Bonjour\n\nle monde" {
		t.Fatalf("got %q, %v", got, err)
	}

	fakeTesseract(t, "List of available languages (1):\nosd\n", "")
	if _, err := Text(context.Background(), "capture.png"); !errors.Is(err, ErrMissing) {
		t.Fatalf("no language: %v", err)
	}

	t.Setenv("PATH", t.TempDir())
	if _, err := Text(context.Background(), "capture.png"); !errors.Is(err, ErrMissing) {
		t.Fatalf("no tesseract: %v", err)
	}
}
