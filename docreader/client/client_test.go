package client

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/docreader/proto"
)

func init() {
	log.SetOutput(os.Stdout)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile)
	log.Println("INFO: Initializing DocReader client tests")
}

func TestDefaultMaxMessageSizeMBAddsTransportHeadroom(t *testing.T) {
	tests := []struct {
		uploadMB int
		wantMB   int
	}{
		{uploadMB: 100, wantMB: 132},
		{uploadMB: 200, wantMB: 250},
		{uploadMB: 20, wantMB: 52},
	}

	for _, tt := range tests {
		if got := DefaultMaxMessageSizeMB(tt.uploadMB); got != tt.wantMB {
			t.Errorf("DefaultMaxMessageSizeMB(%d) = %d, want %d", tt.uploadMB, got, tt.wantMB)
		}
	}
}

func TestGetMaxMessageSizeUsesDerivedTransportHeadroom(t *testing.T) {
	t.Setenv("MAX_FILE_SIZE_MB", "100")
	t.Setenv("DOCREADER_GRPC_MAX_FILE_SIZE_MB", "")

	if got, want := GetMaxMessageSize(), 132*1024*1024; got != want {
		t.Fatalf("GetMaxMessageSize() = %d, want %d", got, want)
	}
}

func TestGetMaxMessageSizeHonorsExplicitTransportLimit(t *testing.T) {
	t.Setenv("MAX_FILE_SIZE_MB", "100")
	t.Setenv("DOCREADER_GRPC_MAX_FILE_SIZE_MB", "180")

	if got, want := GetMaxMessageSize(), 180*1024*1024; got != want {
		t.Fatalf("GetMaxMessageSize() = %d, want %d", got, want)
	}
}

func requireLiveDocReaderClient(t *testing.T) *Client {
	t.Helper()

	client, err := NewClient("localhost:50051")
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := client.ListEngines(ctx, &proto.ListEnginesRequest{}); err != nil {
		t.Skipf("DocReader gRPC server not available at localhost:50051: %v", err)
	}

	return client
}

func TestReadURL(t *testing.T) {
	client := requireLiveDocReaderClient(t)
	client.SetDebug(true)

	startTime := time.Now()
	resp, err := client.Read(
		context.Background(),
		&proto.ReadRequest{
			Url:   "https://example.com",
			Title: "test",
		},
	)
	log.Printf("INFO: Read(URL) completed in %v", time.Since(startTime))

	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("Read returned error: %s", resp.Error)
	}
	if resp.MarkdownContent == "" {
		t.Error("Expected non-empty markdown content")
	}
	log.Printf("INFO: content_len=%d, images=%d", len(resp.MarkdownContent), len(resp.ImageRefs))
}

func TestReadFile(t *testing.T) {
	client := requireLiveDocReaderClient(t)
	client.SetDebug(true)

	fileContent, err := os.ReadFile("../testdata/test.md")
	if err != nil {
		t.Fatalf("Failed to read test file: %v", err)
	}

	startTime := time.Now()
	resp, err := client.Read(
		context.Background(),
		&proto.ReadRequest{
			FileContent: fileContent,
			FileName:    "test.md",
			FileType:    "md",
		},
	)
	log.Printf("INFO: Read(file) completed in %v", time.Since(startTime))

	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("Read returned error: %s", resp.Error)
	}
	if resp.MarkdownContent == "" {
		t.Error("Expected non-empty markdown content")
	}

	imageRefs := GetImageRefsFromResponse(resp)
	log.Printf("INFO: content_len=%d, images=%d", len(resp.MarkdownContent), len(imageRefs))
}
