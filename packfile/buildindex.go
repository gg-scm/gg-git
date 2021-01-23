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
	"crypto/sha1"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"io/ioutil"
	"sort"
	"sync"

	"gg-scm.io/pkg/git/githash"
	"gg-scm.io/pkg/git/object"
)

// BuildIndex indexes a packfile. This is equivalent to running git-index-pack(1)
// on the packfile.
func BuildIndex(f io.ReaderAt, fileSize int64) (*Index, error) {
	fileHash := sha1.New()
	hashTee := &teeByteReader{
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

	// Index deltified objects if needed.
	if !base.hasDeltas() {
		return base.Index, nil
	}
	c := newDeltaCrawler(f, base)
	defer c.wait()
	var rootReader *bufio.Reader
	var z zlibReader
	readBaseObject := func(offset int64) (object.Type, []byte, error) {
		section := io.NewSectionReader(f, offset, fileSize-offset)
		if rootReader == nil {
			rootReader = bufio.NewReader(section)
		} else {
			rootReader.Reset(section)
		}
		hdr, err := readObjectHeader(offset, rootReader)
		if err != nil {
			return "", nil, err
		}
		if err := setZlibReader(&z, rootReader); err != nil {
			return "", nil, err
		}
		buf := bytes.NewBuffer(make([]byte, 0, int(hdr.Size)))
		if _, err := io.Copy(buf, z); err != nil {
			return "", nil, err
		}
		return hdr.Type.NonDelta(), buf.Bytes(), nil
	}

	// First: offsets.
	for _, root := range base.rootOffsets {
		typ, data, err := readBaseObject(root)
		if err != nil {
			return nil, fmt.Errorf("packfile: build index: %w", err)
		}
		c.mu.Lock()
		children := c.childrenByOffset[root]
		delete(c.childrenByOffset, root)
		c.mu.Unlock()
		for _, child := range children {
			c.startCrawl(typ, bytes.NewReader(data), child)
		}
	}

	// Finally: object IDs.
	for {
		c.mu.Lock()
		i := -1
		var children []*deltaObject
		for id, potentialChildren := range c.childrenByID {
			i = base.Index.FindID(id)
			if i != -1 {
				children = potentialChildren
				delete(c.childrenByID, id)
				break
			}
		}
		c.mu.Unlock()
		if i == -1 {
			// All remaining object ID references are to other deltified objects.
			// TODO(soon): Check storage to thicken pack.
			break
		}

		root := base.Index.Offsets[i]
		typ, data, err := readBaseObject(root)
		if err != nil {
			return nil, fmt.Errorf("packfile: build index: %w", err)
		}
		for _, child := range children {
			c.startCrawl(typ, bytes.NewReader(data), child)
		}
	}

	if err := c.wait(); err != nil {
		return nil, fmt.Errorf("packfile: build index: %w", err)
	}
	sort.Sort(c.newIndex)
	return c.newIndex, nil
}

type baseIndex struct {
	*Index
	rootOffsets      []int64
	childrenByOffset map[int64][]*deltaObject
	childrenByID     map[githash.SHA1][]*deltaObject
}

func (base *baseIndex) hasDeltas() bool {
	return len(base.childrenByOffset)+len(base.childrenByID) > 0
}

type deltaObject struct {
	offset      int64
	sectionSize int
	crc32       uint32
}

// maxDeltaObjectSize is the maximum number of bytes that will be held in memory
// for a single object during undeltification. Note that during undeltifying,
// both the base and target object must be held in memory, so the maximum amount
// of memory used will be 2 * maxDeltaObjectSize.
const maxDeltaObjectSize = 16 << 20 // 16 MiB

// basePass indexes any non-deltified objects and builds a tree of deltified
// objects to undeltify.
func baseIndexPass(r *byteReaderCounter, nobjs uint32) (*baseIndex, error) {
	result := &baseIndex{
		Index: &Index{
			ObjectIDs:       make([]githash.SHA1, 0, int(nobjs)),
			Offsets:         make([]int64, 0, int(nobjs)),
			PackedChecksums: make([]uint32, 0, int(nobjs)),
		},
		childrenByOffset: make(map[int64][]*deltaObject),
		childrenByID:     make(map[githash.SHA1][]*deltaObject),
	}
	sizes := make([]int64, 0, int(nobjs))
	sha1Hash := sha1.New()
	c := crc32.NewIEEE()
	t := &teeByteReader{r: r, w: c}
	var z zlibReader
	for ; nobjs > 0; nobjs-- {
		c.Reset()
		hdr, err := readObjectHeader(r.n, t)
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
			if sectionSize > maxDeltaObjectSize {
				return nil, fmt.Errorf("compressed deltified object too large (%d bytes)", hdr.Size)
			}
			dobj := &deltaObject{
				offset:      hdr.Offset,
				sectionSize: int(sectionSize),
				crc32:       c.Sum32(),
			}
			switch hdr.Type {
			case RefDelta:
				result.childrenByID[hdr.BaseObject] = append(result.childrenByID[hdr.BaseObject], dobj)
			case OffsetDelta:
				result.childrenByOffset[hdr.BaseOffset] = append(result.childrenByOffset[hdr.BaseOffset], dobj)
				// While we're building the base index, the Offsets slice is sorted.
				// Since the offsets are always negative, we will know definitively
				// whether the base is deltified or not.
				if i := searchInt64(result.Offsets, hdr.BaseOffset); i != -1 {
					if baseObjectSize := sizes[i]; baseObjectSize > maxDeltaObjectSize {
						return nil, fmt.Errorf("delta object base too large (%d bytes)", baseObjectSize)
					}
					result.rootOffsets = append(result.rootOffsets, hdr.BaseOffset)
				}
			default:
				panic("unreachable")
			}
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
		sizes = append(sizes, hdr.Size)
	}

	// We inserted in offset order. Index is expected to be in object ID order.
	// Sorting in bulk is more efficient than doing an insertion sort and lets
	// us use the Offsets table for various delta-related things above.
	sort.Sort(result.Index)
	return result, nil
}

func searchInt64(slice []int64, x int64) int {
	i := sort.Search(len(slice), func(i int) bool { return slice[i] >= x })
	if i >= len(slice) || slice[i] != x {
		return -1
	}
	return i
}

// deltaCrawler manages a group of goroutines that undeltify objects for indexing.
type deltaCrawler struct {
	f   io.ReaderAt
	sem chan *indexer
	wg  sync.WaitGroup

	errOnce sync.Once
	err     error

	mu               sync.Mutex
	newIndex         *Index // unsorted
	childrenByOffset map[int64][]*deltaObject
	childrenByID     map[githash.SHA1][]*deltaObject
}

func newDeltaCrawler(f io.ReaderAt, base *baseIndex) *deltaCrawler {
	c := &deltaCrawler{
		f:                f,
		sem:              make(chan *indexer, 2),
		newIndex:         new(Index),
		childrenByOffset: base.childrenByOffset,
		childrenByID:     base.childrenByID,
	}
	*c.newIndex = *base.Index
	for i := 0; i < cap(c.sem); i++ {
		c.sem <- new(indexer)
	}
	return c
}

// startCrawl starts a goroutine to index a deltified object and its descendants.
func (c *deltaCrawler) startCrawl(typ object.Type, baseObject io.ReadSeeker, obj *deltaObject) {
	// TODO(someday): Don't start crawling if c.err != nil.
	u := <-c.sem
	c.wg.Add(1)
	go c.crawl(u, typ, baseObject, obj)
}

// crawl indexes a deltified object and its descendants.
func (c *deltaCrawler) crawl(idxr *indexer, typ object.Type, baseObject io.ReadSeeker, obj *deltaObject) {
	defer c.wg.Done()
	parent, children, err := c.process(idxr, typ, baseObject, obj)
	if err != nil {
		c.sem <- idxr // release
		c.errOnce.Do(func() { c.err = err })
		return
	}
	if len(children) == 0 {
		c.sem <- idxr // release
		return
	}
	// The first child inherits the acquired undeltifier.
	c.wg.Add(1)
	go c.crawl(idxr, typ, bytes.NewReader(parent), children[0])
	// Subsequent children must acquire undeltifiers from the semaphore.
	// Each baseObject bytes.Reader must be separate to avoid races.
	for _, child := range children[1:] {
		c.startCrawl(typ, bytes.NewReader(parent), child)
	}
}

// process processes a single deltified object and returns the location of any
// new deltified objects that can be processed as a result.
func (c *deltaCrawler) process(idxr *indexer, typ object.Type, baseObject io.ReadSeeker, obj *deltaObject) ([]byte, []*deltaObject, error) {
	deltaPackObject := make([]byte, obj.sectionSize)
	if _, err := c.f.ReadAt(deltaPackObject, obj.offset); err != nil {
		return nil, nil, err
	}
	id, data, err := idxr.undeltify(typ, baseObject, bytes.NewReader(deltaPackObject))
	if err != nil {
		return nil, nil, err
	}

	var children []*deltaObject
	c.mu.Lock()
	defer c.mu.Unlock()
	c.newIndex.Offsets = append(c.newIndex.Offsets, obj.offset)
	c.newIndex.ObjectIDs = append(c.newIndex.ObjectIDs, id)
	c.newIndex.PackedChecksums = append(c.newIndex.PackedChecksums, obj.crc32)
	children = append(children, c.childrenByOffset[obj.offset]...)
	delete(c.childrenByOffset, obj.offset)
	children = append(children, c.childrenByID[id]...)
	delete(c.childrenByID, id)
	return data, children, nil
}

// wait waits any crawl operations to finish and returns the first error that
// occurred, if any. wait may be called multiple times, but startCrawl must not
// be called after wait returns.
func (c *deltaCrawler) wait() error {
	c.wg.Wait()
	c.errOnce.Do(func() {})
	return c.err
}

// An indexer decompresses deltified objects and computes their IDs.
// The zero value is a valid indexer.
//
// indexer is distinct from Undeltifier because it creates new byte buffers for
// each undeltification. This is crucial for index-building because it permits
// concurrent reads of these buffers without synchronization.
//
// The fields of indexer are expensive to create in a tight loop. Reusing an
// indexer reduces memory allocations.
type indexer struct {
	z    zlibReader
	sha1 hash.Hash
}

func (idxr *indexer) undeltify(typ object.Type, baseObject io.ReadSeeker, deltaPackObject ByteReader) (githash.SHA1, []byte, error) {
	if _, err := ReadHeader(0, deltaPackObject); err != nil {
		return githash.SHA1{}, nil, err
	}
	if err := setZlibReader(&idxr.z, deltaPackObject); err != nil {
		return githash.SHA1{}, nil, err
	}
	defer setZlibReader(&idxr.z, emptyReader{}) // don't retain deltaPackObject past function return
	newObjectReader := NewDeltaReader(baseObject, bufio.NewReader(idxr.z))
	newSize, err := newObjectReader.Size()
	if err != nil {
		return githash.SHA1{}, nil, err
	}
	newObject := bytes.NewBuffer(make([]byte, 0, newSize))
	if _, err := io.Copy(newObject, newObjectReader); err != nil {
		return githash.SHA1{}, nil, err
	}
	if idxr.sha1 == nil {
		idxr.sha1 = sha1.New()
	} else {
		idxr.sha1.Reset()
	}
	idxr.sha1.Write(object.AppendPrefix(nil, typ, int64(newObject.Len())))
	idxr.sha1.Write(newObject.Bytes())
	var sumSHA1 githash.SHA1
	idxr.sha1.Sum(sumSHA1[:0])
	return sumSHA1, newObject.Bytes(), nil
}

type teeByteReader struct {
	r   ByteReader
	w   io.Writer
	buf [1]byte
}

func (t *teeByteReader) Read(p []byte) (int, error) {
	n, rerr := t.r.Read(p)
	_, werr := t.w.Write(p[:n])
	if rerr != nil {
		return n, rerr
	}
	return n, werr
}

func (t *teeByteReader) ReadByte() (byte, error) {
	b, rerr := t.r.ReadByte()
	t.buf[0] = b
	_, werr := t.w.Write(t.buf[:])
	if rerr != nil {
		return b, rerr
	}
	return b, werr
}

type emptyReader struct{}

func (emptyReader) Read(_ []byte) (int, error) {
	return 0, io.EOF
}

func (emptyReader) ReadByte() (byte, error) {
	return 0, io.EOF
}
