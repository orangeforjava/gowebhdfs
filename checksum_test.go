package webhdfs

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestComputeLocalFileChecksumMatchesHadoopCRC32C(t *testing.T) {
	filename := writeChecksumPatternFile(t, 1024*1024)
	got, err := ComputeLocalFileChecksum(filename, ChecksumSpec{
		Algorithm:   "MD5-of-0MD5-of-512CRC32C",
		BlockSize:   128 * 1024 * 1024,
		Workers:     4,
		CRCPerBlock: 0,
		BytesPerCRC: 512,
		CRCType:     "CRC32C",
		Length:      28,
	})
	if err != nil {
		t.Fatal(err)
	}

	const want = "0000020000000000000000000e676adb4261d919c6bb465c056b50cc"
	if got.Bytes != want {
		t.Fatalf("checksum = %s, want %s", got.Bytes, want)
	}
}

func TestCompareLocalFileChecksum(t *testing.T) {
	filename := writeChecksumPatternFile(t, 1024*1024)
	server := mockChecksumServer(t, "0000020000000000000000000e676adb4261d919c6bb465c056b50cc00000000", int64(1024*1024))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	fs, err := NewFileSystem(Configuration{Addr: u.Host, User: "testuser"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := fs.CompareLocalFileChecksum(filename, Path{Name: "/remote/file"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Match || !result.SizeMatch {
		t.Fatalf("result = %+v, want checksum and size match", result)
	}
}

func writeChecksumPatternFile(t *testing.T, size int) string {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "pattern.bin")
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i)
	}
	if err := os.WriteFile(filename, data, 0644); err != nil {
		t.Fatal(err)
	}
	return filename
}

func mockChecksumServer(t *testing.T, checksum string, length int64) *httptest.Server {
	t.Helper()
	handler := func(rsp http.ResponseWriter, req *http.Request) {
		switch req.URL.Query().Get("op") {
		case OP_GETFILESTATUS:
			fmt.Fprintf(rsp, `{"FileStatus":{"blockSize":134217728,"length":%d,"type":"FILE"}}`, length)
		case OP_GETFILECHECKSUM:
			fmt.Fprintf(rsp, `{"FileChecksum":{"algorithm":"MD5-of-0MD5-of-512CRC32C","bytes":"%s","length":28}}`, checksum)
		default:
			t.Fatalf("unexpected op %q", req.URL.Query().Get("op"))
		}
	}
	return httptest.NewServer(http.HandlerFunc(handler))
}
