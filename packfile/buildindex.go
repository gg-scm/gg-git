// Copyright 2021 The gg Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//		 https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package packfile

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/ioutil"
	"os"
	"sort"

	"gg-scm.io/pkg/git/githash"
	"gg-scm.io/pkg/git/object"
)

func BuildIndex(f io.ReaderAt, fileSize int64, storage SHA1ObjectReadWriter) (*Index, error) {
	fileHash := sha1.New()
	hashTee := teeByteReader{
		r: bufio.NewReader(io.NewSectionReader(f, 0, fileSize)),
		w: fileHash,
	}
	nobjs, err := readFileHeader(hashTee)
	if err != nil {
		return nil, fmt.Errorf("packfile: build index: %w", err)
	}

	// Read file serially to get initial index.
	brc := &byteReaderCounter{r: hashTee, n: fileHeaderSize}
	base, err := baseIndexPass(brc, nobjs)
	if err != nil {
		return nil, fmt.Errorf("packfile: build index: %w", err)
	}

	// Verify end-of-packfile SHA-1 hash.
	var gotSum githash.SHA1
	fileHash.Sum(gotSum[:0])
	endOfObjects := brc.n
	if _, err := f.ReadAt(base.PackfileSHA1[:], endOfObjects); err != nil {
		return nil, fmt.Errorf("packfile: build index: %w", err)
	}
	if !bytes.Equal(gotSum[:], base.PackfileSHA1[:]) {
		return nil, fmt.Errorf("packfile: build index: packfile checksum does not match content")
	}
	if endOfObjects+githash.SHA1Size != fileSize {
		return nil, fmt.Errorf("packfile: build index: trailing data in packfile")
	}

	// Index deltified objects.
	// Deltified objects may use other deltified objects as a base, so we sweep
	// over deltified objects until we converge (iterative instead of recursive).
	ds := &deltaSweeper{
		baseIndex: *base,
		fileSize:  fileSize,
		storage:   storage,
	}
	for ds.needsSweep() {
		if err := ds.sweep(f); err != nil {
			return nil, fmt.Errorf("packfile: build index: %w", err)
		}
	}
	return ds.buildIndex(), nil
}

type deltaHeader struct {
	offset      int64
	sectionSize int
	// baseOffset is the Offset of a previous Header for an OffsetDelta type object.
	baseOffset int64
	// baseObject is the hash of an object for a RefDelta type object.
	baseObject githash.SHA1
	crc32      uint32
}

func (dhdr *deltaHeader) typ() ObjectType {
	if dhdr.baseOffset != 0 {
		return OffsetDelta
	}
	return RefDelta
}

type baseIndex struct {
	*Index
	offsetToID   map[int64]githash.SHA1
	deltaHeaders []*deltaHeader
}

// basePass indexes any non-deltified objects.
func baseIndexPass(r *byteReaderCounter, nobjs uint32) (*baseIndex, error) {
	result := &baseIndex{
		Index: &Index{
			ObjectIDs:       make([]githash.SHA1, 0, int(nobjs)),
			Offsets:         make([]int64, 0, int(nobjs)),
			PackedChecksums: make([]uint32, 0, int(nobjs)),
		},
		offsetToID: make(map[int64]githash.SHA1),
	}
	sha1Hash := sha1.New()
	c := crc32.NewIEEE()
	t := teeByteReader{r: r, w: c}
	var z zlibReader
	for ; nobjs > 0; nobjs-- {
		c.Reset()
		hdr, err := ReadHeader(r.n, t)
		if err != nil {
			return nil, err
		}
		if err := setZlibReader(&z, t); err != nil {
			return nil, err
		}
		objType := hdr.Type.NonDelta()
		if objType == "" {
			// Deltified object.
			size, err := io.Copy(ioutil.Discard, z)
			if err != nil {
				return nil, err
			}
			if size < hdr.Size {
				return nil, errTooShort
			}
			if size > hdr.Size {
				return nil, errTooLong
			}
			sectionSize := r.n - hdr.Offset
			if sectionSize > 16<<20 { // 16 MiB
				return nil, fmt.Errorf("compressed deltified object too large (%d bytes)", hdr.Size)
			}
			result.deltaHeaders = append(result.deltaHeaders, &deltaHeader{
				offset:      hdr.Offset,
				sectionSize: int(sectionSize),
				baseOffset:  hdr.BaseOffset,
				baseObject:  hdr.BaseObject,
				crc32:       c.Sum32(),
			})
			continue
		}
		sha1Hash.Reset()
		sha1Hash.Write(object.AppendPrefix(nil, objType, hdr.Size))
		size, err := io.Copy(sha1Hash, z)
		if err != nil {
			return nil, err
		}
		if size < hdr.Size {
			return nil, errTooShort
		}
		if size > hdr.Size {
			return nil, errTooLong
		}
		var sum githash.SHA1
		sha1Hash.Sum(sum[:0])
		result.Offsets = append(result.Offsets, hdr.Offset)
		result.ObjectIDs = append(result.ObjectIDs, sum)
		result.PackedChecksums = append(result.PackedChecksums, c.Sum32())
		result.offsetToID[hdr.Offset] = sum
	}

	// We inserted in offset order. Index is expected to be in object ID order.
	// (Sorting in bulk is more efficient than doing an insertion sort.)
	sort.Sort(result.Index)
	return result, nil
}

type deltaSweeper struct {
	baseIndex
	additions Index // unsorted

	fileSize int64
	storage  SHA1ObjectReadWriter
}

func (ds *deltaSweeper) buildIndex() *Index {
	if ds.additions.Len() > 0 {
		ds.Offsets = append(ds.Offsets, ds.additions.Offsets...)
		ds.ObjectIDs = append(ds.ObjectIDs, ds.additions.ObjectIDs...)
		ds.PackedChecksums = append(ds.PackedChecksums, ds.additions.PackedChecksums...)
		sort.Sort(ds.Index)
		ds.additions = Index{}
	}
	return ds.Index
}

func (ds *deltaSweeper) needsSweep() bool {
	return len(ds.deltaHeaders) > 0
}

func (ds *deltaSweeper) sweep(r io.ReaderAt) error {
	remaining := 0
	sem := make(chan struct{}, 4)
	results := make(chan indexResult)
	var firstErr error
loop:
	for _, dhdr := range ds.deltaHeaders {
		basePrefix, baseObject, err := ds.lookupBaseObject(r, dhdr)
		if errors.Is(err, os.ErrNotExist) {
			// Base is deltified and hasn't been expanded yet.
			// Skip until next sweep.
			ds.deltaHeaders[remaining] = dhdr
			remaining++
			continue
		}
		if err != nil {
			firstErr = err
			break loop
		}
	startIndex:
		for {
			select {
			case sem <- struct{}{}:
				// Acquired semaphore. Ready to start more indexing.
				dhdr := dhdr
				go func() {
					defer func() { <-sem }()
					deltaObject := make([]byte, int(dhdr.sectionSize))
					if _, err := r.ReadAt(deltaObject, dhdr.offset); err != nil {
						baseObject.Close()
						results <- indexResult{err: err}
						return
					}
					sum, err := indexDeltifiedObject(ds.storage, basePrefix, baseObject, dhdr.offset, deltaObject)
					baseObject.Close()
					if err != nil {
						results <- indexResult{err: err}
						return
					}
					results <- indexResult{
						offset:   dhdr.offset,
						sha1:     sum,
						checksum: dhdr.crc32,
					}
				}()
				break startIndex
			case r := <-results:
				// Finished indexing one of the objects.
				if r.err != nil {
					baseObject.Close()
					firstErr = r.err
					break loop
				}
				ds.add(r)
			}
		}
	}

	// Wait until all objects are done being indexed.
	for i := 0; i < cap(sem); {
		select {
		case sem <- struct{}{}:
			i++
		case r := <-results:
			if r.err == nil {
				ds.add(r)
			} else if r.err != nil && firstErr == nil {
				firstErr = r.err
			}
		}
	}
	if firstErr != nil {
		ds.deltaHeaders = nil
		return firstErr
	}
	if remaining == len(ds.deltaHeaders) {
		// TODO(someday): Add details of missing objects
		return fmt.Errorf("unable to un-deltify %d objects", remaining)
	}
	ds.deltaHeaders = ds.deltaHeaders[:remaining]
	return nil
}

type indexResult struct {
	offset   int64
	sha1     githash.SHA1
	checksum uint32
	err      error
}

func indexDeltifiedObject(storage SHA1ObjectReadWriter, basePrefix object.Prefix, baseObject io.ReadSeeker, deltaOffset int64, deltaObject []byte) (githash.SHA1, error) {
	deltaObjectReader := bytes.NewReader(deltaObject)
	if _, err := ReadHeader(deltaOffset, deltaObjectReader); err != nil {
		return githash.SHA1{}, err
	}
	z, err := zlib.NewReader(deltaObjectReader)
	if err != nil {
		return githash.SHA1{}, err
	}
	newObjectReader := NewDeltaReader(baseObject, bufio.NewReader(z))
	newSize, err := newObjectReader.Size()
	if err != nil {
		return githash.SHA1{}, err
	}
	newPrefix := object.Prefix{
		Type: basePrefix.Type,
		Size: newSize,
	}
	newObject, err := storage.WriteSHA1Object(newPrefix)
	if err != nil {
		return githash.SHA1{}, err
	}
	_, copyErr := io.Copy(newObject, newObjectReader)
	sum, finishErr := newObject.FinishObject()
	if copyErr != nil {
		return githash.SHA1{}, copyErr
	}
	if finishErr != nil {
		return githash.SHA1{}, finishErr
	}
	var sumSHA1 githash.SHA1
	copy(sumSHA1[:], sum)
	return sumSHA1, nil
}

func (ib *deltaSweeper) lookupBaseObject(r io.ReaderAt, dhdr *deltaHeader) (object.Prefix, ReadSeekCloser, error) {
	var baseObjectID githash.SHA1
	switch dhdr.typ() {
	case OffsetDelta:
		var ok bool
		baseObjectID, ok = ib.offsetToID[dhdr.baseOffset]
		if !ok {
			// Base is deltified and hasn't been expanded yet.
			return object.Prefix{}, nil, os.ErrNotExist
		}
	case RefDelta:
		baseObjectID = dhdr.baseObject
	default:
		panic("unknown deltified type")
	}
	basePrefix, baseObject, err := ib.storage.ReadSHA1Object(baseObjectID)
	if err == nil {
		return basePrefix, baseObject, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return object.Prefix{}, nil, err
	}
	baseIndex := ib.FindID(baseObjectID)
	if baseIndex == -1 {
		// Base is deltified and hasn't been expanded yet.
		return object.Prefix{}, nil, os.ErrNotExist
	}
	// Not in storage, but is present in index. This means it's one of the objects
	// collected during the base pass.
	baseOffset := ib.Offsets[baseIndex]
	sr := bufio.NewReader(io.NewSectionReader(r, baseOffset, ib.fileSize-baseOffset))
	baseHdr, err := ReadHeader(baseOffset, sr)
	if err != nil {
		return object.Prefix{}, nil, err
	}
	w, err := ib.storage.WriteSHA1Object(object.Prefix{
		Type: baseHdr.Type.NonDelta(),
		Size: baseHdr.Size,
	})
	if err != nil {
		return object.Prefix{}, nil, err
	}
	z, err := zlib.NewReader(sr)
	if err != nil {
		return object.Prefix{}, nil, err
	}
	_, copyErr := io.Copy(w, z)
	gotSum, finishErr := w.FinishObject()
	if copyErr != nil {
		return object.Prefix{}, nil, copyErr
	}
	if finishErr != nil {
		return object.Prefix{}, nil, finishErr
	}
	var got githash.SHA1
	copy(got[:], gotSum)
	if got != baseObjectID {
		return object.Prefix{}, nil, fmt.Errorf("object %v has unexpected SHA-1 hash %v after writing", baseObjectID, got)
	}
	basePrefix, baseObject, err = ib.storage.ReadSHA1Object(baseObjectID)
	if errors.Is(err, os.ErrNotExist) {
		err = fmt.Errorf("object %v does not exist after being written", baseObjectID)
	}
	return basePrefix, baseObject, err
}

func (ds *deltaSweeper) add(r indexResult) {
	ds.additions.Offsets = append(ds.additions.Offsets, r.offset)
	ds.additions.ObjectIDs = append(ds.additions.ObjectIDs, r.sha1)
	ds.additions.PackedChecksums = append(ds.additions.PackedChecksums, r.checksum)
	ds.offsetToID[r.offset] = r.sha1
}

type teeByteReader struct {
	r   ByteReader
	w   io.Writer
	buf [1]byte
}

func (t teeByteReader) Read(p []byte) (int, error) {
	n, rerr := t.r.Read(p)
	_, werr := t.w.Write(p[:n])
	if rerr != nil {
		return n, rerr
	}
	return n, werr
}

func (t teeByteReader) ReadByte() (byte, error) {
	b, rerr := t.r.ReadByte()
	t.buf[0] = b
	_, werr := t.w.Write(t.buf[:])
	if rerr != nil {
		return b, rerr
	}
	return b, werr
}
