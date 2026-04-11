package s3_ingestion

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestBuildLambdaZip(t *testing.T) {
	t.Parallel()

	zipData, err := buildLambdaZip()
	if err != nil {
		t.Fatalf("buildLambdaZip() error: %s", err)
	}

	if len(zipData) == 0 {
		t.Fatal("zip data is empty")
	}

	// Verify it's a valid zip.
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		t.Fatalf("invalid zip: %s", err)
	}

	if len(reader.File) != 1 {
		t.Fatalf("expected 1 file in zip, got %d", len(reader.File))
	}

	if reader.File[0].Name != "lambda_handler.py" {
		t.Errorf("expected file name lambda_handler.py, got %s", reader.File[0].Name)
	}

	// Verify the file has content.
	f, err := reader.File[0].Open()
	if err != nil {
		t.Fatalf("failed to open zip entry: %s", err)
	}
	defer f.Close()

	buf := new(bytes.Buffer)
	buf.ReadFrom(f)
	content := buf.String()

	if len(content) == 0 {
		t.Fatal("lambda_handler.py is empty in zip")
	}

	// Verify it contains the handler function.
	if !bytes.Contains([]byte(content), []byte("def handler(event, context)")) {
		t.Error("lambda_handler.py does not contain handler function")
	}
}

func TestEmbeddedScriptsNotEmpty(t *testing.T) {
	t.Parallel()

	if len(lambdaHandlerScript) == 0 {
		t.Error("embedded lambda_handler.py is empty")
	}

	if len(glueJobScript) == 0 {
		t.Error("embedded glue_job.py is empty")
	}
}
