package webhdfs

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var md5CRCAlgorithmRE = regexp.MustCompile(`^MD5-of-(\d+)MD5-of-(\d+)(CRC32C|CRC32)$`)

type ChecksumSpec struct {
	Algorithm   string
	BlockSize   int64
	Workers     int
	CRCPerBlock uint64
	BytesPerCRC int
	CRCType     string
	Length      int64
}

type LocalChecksum struct {
	Algorithm string
	Bytes     string
	Length    int64
}

type ChecksumCompareResult struct {
	Match          bool
	SizeMatch      bool
	LocalChecksum  string
	RemoteChecksum string
	Algorithm      string
	LocalBytes     int64
	RemoteBytes    int64
	Duration       time.Duration
}

func ChecksumSpecFromRemote(status FileStatus, checksum FileChecksum, workers int) (ChecksumSpec, error) {
	matches := md5CRCAlgorithmRE.FindStringSubmatch(checksum.Algorithm)
	if matches == nil {
		return ChecksumSpec{}, fmt.Errorf("unsupported HDFS checksum algorithm %q", checksum.Algorithm)
	}
	crcPerBlock, err := strconv.ParseUint(matches[1], 10, 64)
	if err != nil {
		return ChecksumSpec{}, err
	}
	bytesPerCRC, err := strconv.Atoi(matches[2])
	if err != nil {
		return ChecksumSpec{}, err
	}
	if bytesPerCRC <= 0 {
		return ChecksumSpec{}, fmt.Errorf("invalid bytesPerCRC %d", bytesPerCRC)
	}
	if status.BlockSize <= 0 && crcPerBlock > 0 {
		status.BlockSize = int64(crcPerBlock) * int64(bytesPerCRC)
	}
	if status.BlockSize <= 0 {
		return ChecksumSpec{}, fmt.Errorf("invalid block size %d", status.BlockSize)
	}
	if workers <= 0 {
		workers = 4
	}
	return ChecksumSpec{
		Algorithm:   checksum.Algorithm,
		BlockSize:   status.BlockSize,
		Workers:     workers,
		CRCPerBlock: crcPerBlock,
		BytesPerCRC: bytesPerCRC,
		CRCType:     matches[3],
		Length:      checksum.Length,
	}, nil
}

func ComputeLocalFileChecksum(filename string, spec ChecksumSpec) (LocalChecksum, error) {
	if spec.Workers <= 0 {
		spec.Workers = 4
	}
	if spec.BlockSize <= 0 && spec.CRCPerBlock > 0 && spec.BytesPerCRC > 0 {
		spec.BlockSize = int64(spec.CRCPerBlock) * int64(spec.BytesPerCRC)
	}
	if spec.BlockSize <= 0 {
		return LocalChecksum{}, fmt.Errorf("invalid block size %d", spec.BlockSize)
	}
	if spec.BytesPerCRC <= 0 {
		return LocalChecksum{}, fmt.Errorf("invalid bytesPerCRC %d", spec.BytesPerCRC)
	}

	file, err := os.Open(filename)
	if err != nil {
		return LocalChecksum{}, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return LocalChecksum{}, err
	}
	if info.Size() == 0 {
		return LocalChecksum{}, fmt.Errorf("empty files are not supported by local HDFS checksum calculator")
	}

	blockCount := int((info.Size() + spec.BlockSize - 1) / spec.BlockSize)
	workers := spec.Workers
	if workers > blockCount {
		workers = blockCount
	}
	type blockJob struct {
		index  int
		offset int64
		length int64
	}
	type blockResult struct {
		index int
		md5   []byte
		err   error
	}

	jobs := make(chan blockJob, blockCount)
	results := make(chan blockResult, blockCount)
	for i := 0; i < blockCount; i++ {
		offset := int64(i) * spec.BlockSize
		jobs <- blockJob{index: i, offset: offset, length: minInt64(spec.BlockSize, info.Size()-offset)}
	}
	close(jobs)

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for job := range jobs {
				blockMD5, err := computeBlockMD5(file, spec, job.offset, job.length, checksumBufferSize(spec))
				results <- blockResult{index: job.index, md5: blockMD5, err: err}
			}
		}()
	}
	wg.Wait()
	close(results)

	blockMD5s := make([][]byte, blockCount)
	for result := range results {
		if result.err != nil {
			return LocalChecksum{}, result.err
		}
		blockMD5s[result.index] = result.md5
	}

	out := newJavaDataOutputBuffer()
	for i, blockMD5 := range blockMD5s {
		if len(blockMD5) == 0 {
			return LocalChecksum{}, fmt.Errorf("missing block checksum for block %d", i)
		}
		out.write(blockMD5)
	}
	fileMD5 := md5.Sum(out.data)
	return LocalChecksum{
		Algorithm: spec.Algorithm,
		Bytes:     encodeHDFSChecksumBytes(spec, fileMD5[:]),
		Length:    spec.Length,
	}, nil
}

func (fs *FileSystem) CompareLocalFileChecksum(localPath string, hdfsPath Path) (ChecksumCompareResult, error) {
	start := time.Now()
	info, err := os.Stat(localPath)
	if err != nil {
		return ChecksumCompareResult{}, err
	}
	status, err := fs.GetFileStatus(hdfsPath)
	if err != nil {
		return ChecksumCompareResult{}, err
	}
	remote, err := fs.GetFileChecksum(hdfsPath)
	if err != nil {
		return ChecksumCompareResult{}, err
	}
	spec, err := ChecksumSpecFromRemote(status, remote, fs.checksumWorkers())
	if err != nil {
		return ChecksumCompareResult{}, err
	}
	local, err := ComputeLocalFileChecksum(localPath, spec)
	if err != nil {
		return ChecksumCompareResult{}, err
	}
	remoteBytes := NormalizeChecksumBytes(remote)
	return ChecksumCompareResult{
		Match:          strings.EqualFold(local.Bytes, remoteBytes),
		SizeMatch:      info.Size() == status.Length,
		LocalChecksum:  local.Bytes,
		RemoteChecksum: remoteBytes,
		Algorithm:      remote.Algorithm,
		LocalBytes:     info.Size(),
		RemoteBytes:    status.Length,
		Duration:       time.Since(start),
	}, nil
}

func NormalizeChecksumBytes(checksum FileChecksum) string {
	trimmed := strings.TrimSpace(checksum.Bytes)
	hexLength := int(checksum.Length) * 2
	if hexLength > 0 && len(trimmed) > hexLength {
		return trimmed[:hexLength]
	}
	return trimmed
}

func (fs *FileSystem) checksumWorkers() int {
	if fs.Config.ChecksumWorkers > 0 {
		return fs.Config.ChecksumWorkers
	}
	return 4
}

func (fs *FileSystem) checksumEnabled() bool {
	return !fs.Config.DisableChecksumVerification
}

func localPathFromReader(data io.Reader) (string, bool) {
	if named, ok := data.(interface{ Name() string }); ok {
		name := named.Name()
		if name != "" {
			return name, true
		}
	}
	return "", false
}

func computeBlockMD5(file *os.File, spec ChecksumSpec, offset, length int64, bufferSize int) ([]byte, error) {
	crcHash, err := newCRCHash(spec.CRCType)
	if err != nil {
		return nil, err
	}
	blockMD5 := md5.New()
	buffer := make([]byte, bufferSize)
	crcBytes := make([]byte, 4)
	remaining := length
	position := offset

	for remaining > 0 {
		readSize := minInt64(int64(len(buffer)), remaining)
		n, err := file.ReadAt(buffer[:readSize], position)
		if n > 0 {
			writeChunkCRCs(blockMD5, crcHash, crcBytes, buffer[:n], spec.BytesPerCRC)
			position += int64(n)
			remaining -= int64(n)
		}
		if err == nil {
			continue
		}
		if err == io.EOF && remaining == 0 {
			break
		}
		return nil, err
	}
	return blockMD5.Sum(nil), nil
}

type streamingHDFSChecksum struct {
	spec             ChecksumSpec
	crcHash          hash.Hash32
	chunk            []byte
	chunkUsed        int
	crcBytes         []byte
	blockMD5         hash.Hash
	blockCRCCount    uint64
	blockDigestBytes *javaDataOutputBuffer
	total            int64
}

func newStreamingHDFSChecksum(spec ChecksumSpec) (*streamingHDFSChecksum, error) {
	crcHash, err := newCRCHash(spec.CRCType)
	if err != nil {
		return nil, err
	}
	return &streamingHDFSChecksum{
		spec:             spec,
		crcHash:          crcHash,
		chunk:            make([]byte, spec.BytesPerCRC),
		crcBytes:         make([]byte, 4),
		blockMD5:         md5.New(),
		blockDigestBytes: newJavaDataOutputBuffer(),
	}, nil
}

func (c *streamingHDFSChecksum) Write(p []byte) (int, error) {
	written := len(p)
	for len(p) > 0 {
		n := minInt(len(p), len(c.chunk)-c.chunkUsed)
		copy(c.chunk[c.chunkUsed:c.chunkUsed+n], p[:n])
		c.chunkUsed += n
		c.total += int64(n)
		p = p[n:]
		if c.chunkUsed == len(c.chunk) {
			c.flushChunk()
		}
	}
	return written, nil
}

func (c *streamingHDFSChecksum) Sum() LocalChecksum {
	if c.chunkUsed > 0 {
		c.flushPartialChunk()
	}
	if c.blockCRCCount > 0 {
		c.flushBlock()
	}
	fileMD5 := md5.Sum(c.blockDigestBytes.data)
	return LocalChecksum{
		Algorithm: c.spec.Algorithm,
		Bytes:     encodeHDFSChecksumBytes(c.spec, fileMD5[:]),
		Length:    c.spec.Length,
	}
}

func (c *streamingHDFSChecksum) flushChunk() {
	c.crcHash.Reset()
	c.crcHash.Write(c.chunk)
	binary.BigEndian.PutUint32(c.crcBytes, c.crcHash.Sum32())
	c.blockMD5.Write(c.crcBytes)
	c.chunkUsed = 0
	c.blockCRCCount++
	if c.spec.CRCPerBlock > 0 && c.blockCRCCount == c.spec.CRCPerBlock {
		c.flushBlock()
	}
}

func (c *streamingHDFSChecksum) flushPartialChunk() {
	c.crcHash.Reset()
	c.crcHash.Write(c.chunk[:c.chunkUsed])
	binary.BigEndian.PutUint32(c.crcBytes, c.crcHash.Sum32())
	c.blockMD5.Write(c.crcBytes)
	c.chunkUsed = 0
	c.blockCRCCount++
}

func (c *streamingHDFSChecksum) flushBlock() {
	c.blockDigestBytes.write(c.blockMD5.Sum(nil))
	c.blockMD5.Reset()
	c.blockCRCCount = 0
}

func writeChunkCRCs(dst hash.Hash, crcHash hash.Hash32, crcBytes []byte, data []byte, bytesPerCRC int) {
	for len(data) > 0 {
		n := minInt(bytesPerCRC, len(data))
		crcHash.Reset()
		crcHash.Write(data[:n])
		binary.BigEndian.PutUint32(crcBytes, crcHash.Sum32())
		dst.Write(crcBytes)
		data = data[n:]
	}
}

func newCRCHash(crcType string) (hash.Hash32, error) {
	switch crcType {
	case "CRC32C":
		return crc32.New(crc32.MakeTable(crc32.Castagnoli)), nil
	case "CRC32":
		return crc32.NewIEEE(), nil
	default:
		return nil, fmt.Errorf("unsupported crc type %q", crcType)
	}
}

func checksumBufferSize(spec ChecksumSpec) int {
	const defaultSize = 8 * 1024 * 1024
	return (defaultSize / spec.BytesPerCRC) * spec.BytesPerCRC
}

type javaDataOutputBuffer struct {
	data  []byte
	count int
}

func newJavaDataOutputBuffer() *javaDataOutputBuffer {
	return &javaDataOutputBuffer{data: make([]byte, 32)}
}

func (b *javaDataOutputBuffer) write(value []byte) {
	newCount := b.count + len(value)
	if newCount > len(b.data) {
		newData := make([]byte, maxInt(len(b.data)<<1, newCount))
		copy(newData, b.data[:b.count])
		b.data = newData
	}
	copy(b.data[b.count:newCount], value)
	b.count = newCount
}

func encodeHDFSChecksumBytes(spec ChecksumSpec, md5Bytes []byte) string {
	buf := make([]byte, 0, 28)
	tmp4 := make([]byte, 4)
	tmp8 := make([]byte, 8)
	binary.BigEndian.PutUint32(tmp4, uint32(spec.BytesPerCRC))
	buf = append(buf, tmp4...)
	binary.BigEndian.PutUint64(tmp8, spec.CRCPerBlock)
	buf = append(buf, tmp8...)
	buf = append(buf, md5Bytes...)
	return hex.EncodeToString(buf)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
