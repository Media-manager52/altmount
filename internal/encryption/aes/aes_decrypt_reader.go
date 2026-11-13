package aes

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"io"
	"sync"
)

// aesDecryptReader wraps an io.ReadCloser with AES-CBC decryption
// Based on the implementation from rardecode example: github.com/javi11/rardecode/blob/main/examples/rarextract/main.go
type aesDecryptReader struct {
	ctx           context.Context
	getReader     func(ctx context.Context, start, end int64) (io.ReadCloser, error)
	key           []byte
	iv            []byte
	origIV        []byte // Original IV for recalculation during seeks
	decrypter     cipher.BlockMode
	buffer        []byte // Buffer for partial block reads
	bufferPos     int    // Current position in buffer
	bufferLen     int    // Length of valid data in buffer
	offset        int64  // Current read position
	size          int64  // Total size of decrypted data (for output limiting)
	encryptedSize int64  // Total size of encrypted data (for source reading)
	requestEnd    int64  // Caller's desired end offset in decrypted space (-1 = unbounded)

	// mu guards source and closed. Close may run on another goroutine while a
	// Read is blocked (readFullContext force-closes on context timeout), so
	// those two fields must never be read or written without it. The lock is
	// never held across a blocking read, so Close can always interrupt one.
	mu     sync.Mutex
	source io.ReadCloser
	closed bool
}

// newAesDecryptReader creates a new AES-CBC decrypt reader
// decryptedSize is the actual file size (for output limiting)
// encryptedSize is the padded size in segments (for source reading)
// requestEnd is the caller's desired end offset in decrypted space (-1 = unbounded)
func newAesDecryptReader(
	ctx context.Context,
	getReader func(ctx context.Context, start, end int64) (io.ReadCloser, error),
	key, iv []byte,
	decryptedSize, encryptedSize int64,
	requestEnd int64,
) (*aesDecryptReader, error) {
	if len(key) != 16 && len(key) != 24 && len(key) != 32 {
		return nil, fmt.Errorf("invalid AES key size: %d (must be 16, 24, or 32 bytes)", len(key))
	}

	if len(iv) != aes.BlockSize {
		return nil, fmt.Errorf("invalid IV size: %d (must be %d bytes)", len(iv), aes.BlockSize)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	// Make a copy of IV since CBC mode modifies it
	ivCopy := make([]byte, len(iv))
	copy(ivCopy, iv)

	return &aesDecryptReader{
		ctx:           ctx,
		getReader:     getReader,
		source:        nil, // Will be initialized lazily on first read
		key:           key,
		iv:            ivCopy,
		origIV:        iv,
		decrypter:     cipher.NewCBCDecrypter(block, ivCopy),
		buffer:        make([]byte, aes.BlockSize*64), // Buffer multiple blocks for efficiency
		size:          decryptedSize,
		encryptedSize: encryptedSize,
		requestEnd:    requestEnd,
	}, nil
}

// currentSource returns the source and whether the reader is closed.
func (r *aesDecryptReader) currentSource() (io.ReadCloser, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.source, r.closed
}

// installSource makes src the current source. If Close ran while src was
// being created, src is closed instead and io.ErrClosedPipe is returned.
func (r *aesDecryptReader) installSource(src io.ReadCloser) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		src.Close()
		return io.ErrClosedPipe
	}
	r.source = src
	r.mu.Unlock()
	return nil
}

// detachSource removes and returns the current source.
func (r *aesDecryptReader) detachSource() io.ReadCloser {
	r.mu.Lock()
	defer r.mu.Unlock()
	src := r.source
	r.source = nil
	return src
}

// Read implements io.Reader
func (r *aesDecryptReader) Read(p []byte) (int, error) {
	source, closed := r.currentSource()
	if closed {
		return 0, io.ErrClosedPipe
	}

	// Lazy initialization of source reader
	if source == nil {
		sourceEnd := r.encryptedSize - 1
		if r.requestEnd >= 0 {
			encEnd := EncryptedSize(r.requestEnd+1) - 1
			if encEnd < sourceEnd {
				sourceEnd = encEnd
			}
		}
		created, err := r.getReader(r.ctx, 0, sourceEnd)
		if err != nil {
			return 0, fmt.Errorf("failed to initialize source reader: %w", err)
		}
		if err := r.installSource(created); err != nil {
			return 0, err
		}
		source = created
	}

	totalRead := 0

	// Respect requestEnd for output limiting: if the caller requested a sub-range,
	// stop at requestEnd+1 rather than at the full decrypted file size.
	effectiveSize := r.size
	if r.requestEnd >= 0 && r.requestEnd+1 < r.size {
		effectiveSize = r.requestEnd + 1
	}

	for totalRead < len(p) {
		// First, drain any buffered data
		if r.bufferPos < r.bufferLen {
			n := copy(p[totalRead:], r.buffer[r.bufferPos:r.bufferLen])
			r.bufferPos += n
			r.offset += int64(n)
			totalRead += n
			continue
		}

		// Need to read more data
		// Read in multiples of AES block size
		readSize := len(r.buffer)
		if r.offset+int64(readSize) > effectiveSize {
			readSize = int(effectiveSize - r.offset)
			// Round up to block size
			if readSize%aes.BlockSize != 0 {
				readSize += aes.BlockSize - (readSize % aes.BlockSize)
			}
		}

		if readSize == 0 {
			if totalRead > 0 {
				return totalRead, nil
			}
			return 0, io.EOF
		}

		// Read encrypted data
		encBuf := make([]byte, readSize)
		n, err := io.ReadFull(source, encBuf)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return totalRead, err
		}

		if n == 0 {
			if totalRead > 0 {
				return totalRead, nil
			}
			return 0, io.EOF
		}

		// Ensure we have a complete block
		if n%aes.BlockSize != 0 {
			n = (n / aes.BlockSize) * aes.BlockSize
		}

		if n > 0 {
			// Decrypt the data in-place
			r.decrypter.CryptBlocks(encBuf[:n], encBuf[:n])

			// Calculate how much decrypted data is actually part of the file
			decryptedLen := n
			if r.offset+int64(n) > effectiveSize {
				decryptedLen = int(effectiveSize - r.offset)
			}

			// Copy to buffer
			copy(r.buffer, encBuf[:decryptedLen])
			r.bufferLen = decryptedLen
			r.bufferPos = 0
		}

		if err == io.EOF || err == io.ErrUnexpectedEOF {
			if r.bufferLen == 0 {
				if totalRead > 0 {
					return totalRead, nil
				}
				return 0, io.EOF
			}
		}
	}

	return totalRead, nil
}

// Seek implements io.Seeker
// For CBC mode, seeking requires recalculating the IV based on the previous ciphertext block
func (r *aesDecryptReader) Seek(offset int64, whence int) (int64, error) {
	if _, closed := r.currentSource(); closed {
		return 0, io.ErrClosedPipe
	}

	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = r.offset + offset
	case io.SeekEnd:
		abs = r.size + offset
	default:
		return 0, fmt.Errorf("invalid whence: %d", whence)
	}

	if abs < 0 {
		return 0, fmt.Errorf("negative seek position: %d", abs)
	}

	if abs > r.size {
		return 0, fmt.Errorf("seek beyond end of file: %d > %d", abs, r.size)
	}

	// If seeking to current position, nothing to do
	if abs == r.offset {
		return abs, nil
	}

	// Close the current source if it exists
	if old := r.detachSource(); old != nil {
		old.Close()
	}

	// Seeking requires recreating the decrypter with the correct IV
	// For CBC mode: IV for block N = ciphertext of block N-1
	blockNum := abs / int64(aes.BlockSize)
	blockOffset := abs % int64(aes.BlockSize)

	// Calculate bounded source end
	sourceEnd := r.encryptedSize - 1
	if r.requestEnd >= 0 {
		encEnd := EncryptedSize(r.requestEnd+1) - 1
		if encEnd < sourceEnd {
			sourceEnd = encEnd
		}
	}

	// For blockNum > 0, start one block earlier to read the previous ciphertext
	// block as IV, avoiding a separate reader (and duplicate segment download).
	var newIV []byte
	var sourceOffset int64

	if blockNum == 0 {
		newIV = make([]byte, len(r.origIV))
		copy(newIV, r.origIV)
		sourceOffset = 0
	} else {
		// Start from previous block to include IV
		sourceOffset = (blockNum - 1) * int64(aes.BlockSize)
	}

	newSource, err := r.getReader(r.ctx, sourceOffset, sourceEnd)
	if err != nil {
		return 0, fmt.Errorf("failed to get reader for seek position: %w", err)
	}

	if blockNum > 0 {
		// Read previous block as IV from the combined source reader
		newIV = make([]byte, aes.BlockSize)
		_, err = io.ReadFull(newSource, newIV)
		if err != nil {
			newSource.Close()
			return 0, fmt.Errorf("failed to read IV block: %w", err)
		}
		// newSource is now positioned at blockStart, ready for decryption
	}

	// Recreate decrypter with new IV
	block, err := aes.NewCipher(r.key)
	if err != nil {
		newSource.Close()
		return 0, fmt.Errorf("failed to recreate cipher: %w", err)
	}

	if err := r.installSource(newSource); err != nil {
		return 0, err
	}
	r.iv = newIV
	r.decrypter = cipher.NewCBCDecrypter(block, newIV)
	r.offset = blockNum * int64(aes.BlockSize)

	// Reset buffer
	r.bufferPos = 0
	r.bufferLen = 0

	// If we need to skip bytes within the block
	if blockOffset > 0 {
		skipBuf := make([]byte, blockOffset)
		_, err := io.ReadFull(r, skipBuf)
		if err != nil {
			return 0, fmt.Errorf("failed to skip to offset in block: %w", err)
		}
	}

	return abs, nil
}

// Close implements io.Closer
func (r *aesDecryptReader) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	source := r.source
	r.source = nil
	r.mu.Unlock()

	if source != nil {
		return source.Close()
	}

	return nil
}
