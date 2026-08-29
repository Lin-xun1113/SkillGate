package runner

import (
	"encoding/json"
	"testing"
)

func TestDecodeWorkerArtifactsAcceptsProtocolList(t *testing.T) {
	got, err := decodeWorkerArtifacts(json.RawMessage(`[{"name":"result.txt","content_hash":"sha256:x","size_bytes":2}]`))
	if err != nil {
		t.Fatal(err)
	}
	if got["result.txt"] != "sha256:x" {
		t.Fatalf("unexpected artifacts: %#v", got)
	}
}

func TestDecodeWorkerArtifactsAcceptsLegacyObject(t *testing.T) {
	got, err := decodeWorkerArtifacts(json.RawMessage(`{"result.txt":"sha256:x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got["result.txt"] != "sha256:x" {
		t.Fatalf("unexpected artifacts: %#v", got)
	}
}

func TestDecodeWorkerArtifactsRejectsUnnamedArtifact(t *testing.T) {
	if _, err := decodeWorkerArtifacts(json.RawMessage(`[{"content_hash":"sha256:x"}]`)); err == nil {
		t.Fatal("expected unnamed artifact rejection")
	}
}
