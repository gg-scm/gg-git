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
	"bytes"
	"crypto/sha1"
	"fmt"
	"io"
	"io/ioutil"
	"sort"

	"gg-scm.io/pkg/git/githash"
)

/*
On the feasibility of fitting a packfile index in memory:

As of 2021-01-13, the Git repository has ~302K objects and
the Linux kernel repository has 7.8M objects.

We are storing 32 bytes per each object, so even if the entire Linux kernel
history was encoded into one packfile, we would only require ~250MB of RAM
and the array of offsets would still fit in 32-bit indices with plenty of
head room.
*/

// Index is an in-memory mapping of object IDs to offsets within a packfile.
// This maps 1:1 with index files produced by git-index-pack(1).
type Index struct {
	// ObjectIDs is a sorted list of object IDs in the packfile.
	ObjectIDs []githash.SHA1
	// Offsets holds the offsets from the start of the packfile that an object
	// header starts at. The i'th element of Offsets corresponds with the
	// i'th element of ObjectIDs.
	Offsets []int64
	// PackedChecksums holds the CRC32 checksums of each packfile object header
	// and its zlib-compressed contents. The i'th element of PackedChecksums
	// corresponds with the i'th element of ObjectIDs. Version 1 index files do
	// not have this information.
	PackedChecksums []uint32
	// PackfileSHA1 is a copy of the SHA-1 hash present at the end of the packfile.
	PackfileSHA1 githash.SHA1
}

var indexV2Magic = [...]byte{
	0o377, 't', 'O', 'c',
	0, 0, 0, 2,
}

// ReadIndex parses a packfile index file from r. It performs no buffering and
// will not read more bytes than necessary.
func ReadIndex(r io.Reader) (*Index, error) {
	h := sha1.New()
	r = io.TeeReader(r, h)

	first := make([]byte, len(indexV2Magic))
	if _, err := readFull(r, first); err != nil {
		return nil, fmt.Errorf("read packfile index: %w", err)
	}

	var idx *Index
	var err error
	if bytes.Equal(first, indexV2Magic[:]) {
		idx, err = readIndexV2(r)
	} else {
		idx, err = readIndexV1(io.MultiReader(bytes.NewReader(first), r))
	}
	if err != nil {
		return nil, err
	}

	// Read final "checksum".
	got := h.Sum(nil)
	want := make([]byte, len(got))
	if _, err := readFull(r, want); err != nil {
		return nil, err
	}
	if !bytes.Equal(got, want) {
		return nil, fmt.Errorf("read packfile index: checksum does not match")
	}
	return idx, nil
}

// UnmarshalBinary decodes Git's packfile index format into idx.
func (idx *Index) UnmarshalBinary(data []byte) error {
	newIndex, err := ReadIndex(bytes.NewReader(data))
	if err != nil {
		return err
	}
	*idx = *newIndex
	return nil
}

const largeOffsetEntryMask = 1 << 31

func readIndexV2(r io.Reader) (*Index, error) {
	nobjs, err := readIndexObjectCount(r)
	if err != nil {
		return nil, fmt.Errorf("read packfile index: %w", err)
	}
	idx := &Index{
		ObjectIDs:       make([]githash.SHA1, 0, int(nobjs)),
		Offsets:         make([]int64, 0, int(nobjs)),
		PackedChecksums: make([]uint32, 0, int(nobjs)),
	}
	for len(idx.ObjectIDs) < int(nobjs) {
		i := len(idx.ObjectIDs)
		idx.ObjectIDs = idx.ObjectIDs[:i+1]
		if _, err := readFull(r, idx.ObjectIDs[i][:]); err != nil {
			return nil, fmt.Errorf("read packfile index: object ids: %w", err)
		}
	}
	var buf [8]byte
	for len(idx.PackedChecksums) < int(nobjs) {
		if _, err := readFull(r, buf[:4]); err != nil {
			return nil, fmt.Errorf("read packfile index: checksums: %w", err)
		}
		idx.PackedChecksums = append(idx.PackedChecksums, ntohl(buf[:]))
	}
	var largeOffsetEntries []int
	for len(idx.Offsets) < int(nobjs) {
		if _, err := readFull(r, buf[:4]); err != nil {
			return nil, fmt.Errorf("read packfile index: offsets: %w", err)
		}
		off := ntohl(buf[:])
		if off&largeOffsetEntryMask != 0 {
			entIdx := int(off &^ largeOffsetEntryMask)
			if entIdx >= len(largeOffsetEntries) {
				// TODO(someday): This probably does too many allocations.
				newEntries := make([]int, entIdx+1)
				copy(newEntries, largeOffsetEntries)
				for i := len(largeOffsetEntries); i < len(newEntries); i++ {
					newEntries[i] = -1
				}
				largeOffsetEntries = newEntries
			}
			largeOffsetEntries[entIdx] = len(idx.Offsets)
			idx.Offsets = append(idx.Offsets, 0)
			continue
		}
		idx.Offsets = append(idx.Offsets, int64(off))
	}
	for _, i := range largeOffsetEntries {
		if _, err := readFull(r, buf[:8]); err != nil {
			return nil, fmt.Errorf("read packfile index: large offsets: %w", err)
		}
		if i < 0 {
			// Unused entry.
			continue
		}
		off := ntohll(buf[:])
		if off&(1<<63) != 0 {
			return nil, fmt.Errorf("read packfile index: large offsets: overflows int64")
		}
		idx.Offsets[i] = int64(off)
	}
	if _, err := readFull(r, idx.PackfileSHA1[:]); err != nil {
		return nil, fmt.Errorf("read packfile index: packfile sha-1: %w", err)
	}
	return idx, nil
}

func readIndexV1(r io.Reader) (*Index, error) {
	nobjs, err := readIndexObjectCount(r)
	if err != nil {
		return nil, fmt.Errorf("read packfile index: %w", err)
	}
	idx := &Index{
		ObjectIDs: make([]githash.SHA1, 0, int(nobjs)),
		Offsets:   make([]int64, 0, int(nobjs)),
	}
	var offBuf [4]byte
	for len(idx.ObjectIDs) < int(nobjs) {
		if _, err := readFull(r, offBuf[:]); err != nil {
			return nil, fmt.Errorf("read packfile index: entries: %w", err)
		}
		idx.Offsets = append(idx.Offsets, int64(ntohl(offBuf[:])))

		i := len(idx.ObjectIDs)
		idx.ObjectIDs = idx.ObjectIDs[:i+1]
		if _, err := readFull(r, idx.ObjectIDs[i][:]); err != nil {
			return nil, fmt.Errorf("read packfile index: entries: %w", err)
		}
	}
	if _, err := readFull(r, idx.PackfileSHA1[:]); err != nil {
		return nil, fmt.Errorf("read packfile index: packfile sha-1: %w", err)
	}
	return idx, nil
}

const fanOutEntryCount = 256

func readIndexObjectCount(r io.Reader) (uint32, error) {
	if _, err := io.CopyN(ioutil.Discard, r, (fanOutEntryCount-1)*4); err != nil {
		return 0, fmt.Errorf("fanout table: %w", err)
	}
	var raw [4]byte
	if _, err := readFull(r, raw[:]); err != nil {
		return 0, fmt.Errorf("fanout table: %w", err)
	}
	return ntohl(raw[:]), nil
}

// readFull is the same as io.ReadFull but returns io.ErrUnexpectedEOF instead
// of io.EOF.
func readFull(r io.Reader, buf []byte) (int, error) {
	n, err := io.ReadFull(r, buf)
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}

// EncodeV2 writes idx in Git's packfile index version 2 format.
func (idx *Index) EncodeV2(w io.Writer) error {
	if err := idx.validate(); err != nil {
		return fmt.Errorf("write packfile index: %w", err)
	}
	if len(idx.PackedChecksums) != len(idx.ObjectIDs) {
		return fmt.Errorf("number of checksums (%d) different than number of objects (%d)",
			len(idx.PackedChecksums), len(idx.ObjectIDs))
	}
	h := sha1.New()
	wh := io.MultiWriter(w, h)
	if _, err := wh.Write(indexV2Magic[:]); err != nil {
		return fmt.Errorf("write packfile index: %w", err)
	}
	if err := idx.encodeFanOut(wh); err != nil {
		return fmt.Errorf("write packfile index: %w", err)
	}
	for i := range idx.ObjectIDs {
		if _, err := wh.Write(idx.ObjectIDs[i][:]); err != nil {
			return fmt.Errorf("write packfile index: %w", err)
		}
	}
	var buf [githash.SHA1Size]byte
	for _, checksum := range idx.PackedChecksums {
		htonl(buf[:], checksum)
		if _, err := wh.Write(buf[:4]); err != nil {
			return fmt.Errorf("write packfile index: %w", err)
		}
	}
	largeOffsets := 0
	const largeOffsetMin = 1 << 31
	for _, off := range idx.Offsets {
		if off >= largeOffsetMin {
			// Large offset.
			htonl(buf[:4], (1<<31)|uint32(largeOffsets))
			largeOffsets++
		} else {
			htonl(buf[:4], uint32(off))
		}
		if _, err := wh.Write(buf[:4]); err != nil {
			return fmt.Errorf("write packfile index: %w", err)
		}
	}
	if largeOffsets > 0 {
		for _, off := range idx.Offsets {
			if off < largeOffsetMin {
				continue
			}
			htonll(buf[:], uint64(off))
			if _, err := wh.Write(buf[:8]); err != nil {
				return fmt.Errorf("write packfile index: %w", err)
			}
		}
	}
	if _, err := wh.Write(idx.PackfileSHA1[:]); err != nil {
		return fmt.Errorf("write packfile index: %w", err)
	}
	if _, err := w.Write(h.Sum(buf[:0])); err != nil {
		return fmt.Errorf("write packfile index: %w", err)
	}
	return nil
}

// EncodeV1 writes idx in Git's packfile index version 1 format. This generally
// should only be used for compatibility, since the version 1 format does not
// store PackedChecksums and do not support packfiles larger than 4 GiB.
func (idx *Index) EncodeV1(w io.Writer) error {
	if err := idx.validate(); err != nil {
		return fmt.Errorf("write packfile index: %w", err)
	}
	h := sha1.New()
	wh := io.MultiWriter(w, h)
	for _, off := range idx.Offsets {
		if off >= 1<<33 {
			return fmt.Errorf("write packfile index: using version 1 for packfile larger than 4 GiB (found %d offset)", off)
		}
	}
	if err := idx.encodeFanOut(wh); err != nil {
		return fmt.Errorf("write packfile index: %w", err)
	}
	var buf [4 + githash.SHA1Size]byte
	for i, off := range idx.Offsets {
		htonl(buf[:4], uint32(off))
		copy(buf[4:], idx.ObjectIDs[i][:])
		if _, err := wh.Write(buf[:]); err != nil {
			return fmt.Errorf("write packfile index: %w", err)
		}
	}
	if _, err := wh.Write(idx.PackfileSHA1[:]); err != nil {
		return fmt.Errorf("write packfile index: %w", err)
	}
	if _, err := w.Write(h.Sum(buf[:0])); err != nil {
		return fmt.Errorf("write packfile index: %w", err)
	}
	return nil
}

func (idx *Index) validate() error {
	if len(idx.ObjectIDs) != len(idx.Offsets) {
		return fmt.Errorf("number of object IDs (%d) different than number of offsets (%d)",
			len(idx.ObjectIDs), len(idx.Offsets))
	}
	if len(idx.ObjectIDs) > 1 {
		for prevIdx, curr := range idx.ObjectIDs[1:] {
			prev := idx.ObjectIDs[prevIdx]
			if result := bytes.Compare(prev[:], curr[:]); result > 0 {
				return fmt.Errorf("not sorted by object ID")
			} else if result == 0 {
				return fmt.Errorf("object IDs duplicated")
			}
		}
	}
	return nil
}

func (idx *Index) encodeFanOut(w io.Writer) error {
	bucket := int16(0)
	var ent [4]byte
	for i, id := range idx.ObjectIDs {
		if bucket >= int16(id[0]) {
			continue
		}
		htonl(ent[:], uint32(i))
		for ; bucket < int16(id[0]); bucket++ {
			if _, err := w.Write(ent[:]); err != nil {
				return err
			}
		}
	}
	htonl(ent[:], uint32(len(idx.ObjectIDs)))
	for ; bucket < fanOutEntryCount; bucket++ {
		if _, err := w.Write(ent[:]); err != nil {
			return err
		}
	}
	return nil
}

// MarshalBinary encodes the index in Git's packfile index version 2 format.
func (idx *Index) MarshalBinary() ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := idx.EncodeV2(buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// FindID finds the position of id in idx.ObjectIDs or -1 if the ID is not
// present in the index. The result is undefined if idx.ObjectIDs is not sorted.
// This search is O(log len(idx.ObjectIDs)).
func (idx *Index) FindID(id githash.SHA1) int {
	i := idx.findID(id)
	if i >= len(idx.ObjectIDs) || idx.ObjectIDs[i] != id {
		return -1
	}
	return i
}

func (idx *Index) findID(id githash.SHA1) int {
	return sort.Search(len(idx.ObjectIDs), func(i int) bool {
		return bytes.Compare(idx.ObjectIDs[i][:], id[:]) >= 0
	})
}

// FindOffset finds the position of offset in idx.Offsets or -1 if the offset
// is not present in the index. This search is O(len(idx.Offsets)).
func (idx *Index) FindOffset(offset int64) int {
	for i, o := range idx.Offsets {
		if o == offset {
			return i
		}
	}
	return -1
}

// insert inserts the given row into the three tables. It assumes that the
// tables have enough capacity to a new row.
func (idx *Index) insert(off int64, id githash.SHA1, checksum uint32) {
	i := idx.findID(id)
	if i < len(idx.ObjectIDs) && idx.ObjectIDs[i] == id {
		return
	}

	idx.Offsets = idx.Offsets[:len(idx.Offsets)+1]
	copy(idx.Offsets[i+1:], idx.Offsets[i:])
	idx.Offsets[i] = off

	idx.ObjectIDs = idx.ObjectIDs[:len(idx.ObjectIDs)+1]
	copy(idx.ObjectIDs[i+1:], idx.ObjectIDs[i:])
	idx.ObjectIDs[i] = id

	idx.PackedChecksums = idx.PackedChecksums[:len(idx.PackedChecksums)+1]
	copy(idx.PackedChecksums[i+1:], idx.PackedChecksums[i:])
	idx.PackedChecksums[i] = checksum
}

// Len returns the number of objects in the index.
func (idx *Index) Len() int {
	return len(idx.ObjectIDs)
}

// Less returns whether the i'th object ID is lexicographically less than the
// j'th object ID.
func (idx *Index) Less(i, j int) bool {
	return bytes.Compare(idx.ObjectIDs[i][:], idx.ObjectIDs[j][:]) < 0
}

// Swap swaps the i'th and j'th rows of the index.
func (idx *Index) Swap(i, j int) {
	idx.ObjectIDs[i], idx.ObjectIDs[j] = idx.ObjectIDs[j], idx.ObjectIDs[i]
	idx.Offsets[i], idx.Offsets[j] = idx.Offsets[j], idx.Offsets[i]
	if len(idx.PackedChecksums) > 0 {
		idx.PackedChecksums[i], idx.PackedChecksums[j] = idx.PackedChecksums[j], idx.PackedChecksums[i]
	}
}

func (idx *Index) hasOffset(off int64) bool {
	for _, elem := range idx.Offsets {
		if elem == off {
			return true
		}
	}
	return false
}

// ntohl converts a network byte order (big-endian) uint32 and converts it to
// a uint32.
func ntohl(x []byte) uint32 {
	return uint32(x[0])<<24 |
		uint32(x[1])<<16 |
		uint32(x[2])<<8 |
		uint32(x[3])
}

// ntohll converts a network byte order (big-endian) uint64 and converts it to
// a uint64.
func ntohll(x []byte) uint64 {
	return uint64(x[0])<<56 |
		uint64(x[1])<<48 |
		uint64(x[2])<<40 |
		uint64(x[3])<<32 |
		uint64(x[4])<<24 |
		uint64(x[5])<<16 |
		uint64(x[6])<<8 |
		uint64(x[7])
}

// htonl converts a uint32 to a network byte order (big-endian) uint32.
func htonl(buf []byte, x uint32) {
	buf[0] = byte(x >> 24)
	buf[1] = byte(x >> 16)
	buf[2] = byte(x >> 8)
	buf[3] = byte(x)
}

// htonll converts a uint64 to a network byte order (big-endian) uint64.
func htonll(buf []byte, x uint64) {
	buf[0] = byte(x >> 56)
	buf[1] = byte(x >> 48)
	buf[2] = byte(x >> 40)
	buf[3] = byte(x >> 32)
	buf[4] = byte(x >> 24)
	buf[5] = byte(x >> 16)
	buf[6] = byte(x >> 8)
	buf[7] = byte(x)
}
