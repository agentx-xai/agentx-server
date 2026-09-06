package s3

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

func TestMinIOArtifactRoundTrip(t *testing.T) {
	endpoint := os.Getenv("AGENTX_S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("AGENTX_S3_TEST_ENDPOINT is not set")
	}
	store, err := NewArtifactStore(endpoint, os.Getenv("AGENTX_S3_TEST_ACCESS_KEY"), os.Getenv("AGENTX_S3_TEST_SECRET_KEY"), os.Getenv("AGENTX_S3_TEST_BUCKET"), false)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.EnsureBucket(ctx); err != nil {
		t.Fatal(err)
	}
	release, err := store.Put(ctx, "demo", strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Delete(ctx, release.SHA256)
	reader, err := store.Open(ctx, release.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil || string(data) != "hello" {
		t.Fatalf("got %q err %v", data, err)
	}
}
