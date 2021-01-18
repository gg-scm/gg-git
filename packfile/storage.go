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
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"gg-scm.io/pkg/git/githash"
	"gg-scm.io/pkg/git/object"
)

// WriteFinisher combines io.Writer with an method for closing the writer
// and obtaining its SHA-1 hash.
//
// FinishObject finishes writing the object and if successful, returns its SHA-1
// hash. The behavior of FinishObject after the first call is undefined.
// Specific implementations may document their own behavior.
type WriteFinisher interface {
	io.Writer
	FinishObject() ([]byte, error)
}

// SHA1ObjectReadWriter reads and writes entire objects. The ReadSHA1Object and
// WriteSHA1Object methods may be called concurrently with each other.
type SHA1ObjectReadWriter interface {
	// ReadSHA1Object opens an object from storage. If the object does not exist
	// in storage, ReadObject must return an error for which
	// errors.Is(err, os.ErrNotExist) reports true.
	ReadSHA1Object(id githash.SHA1) (object.Prefix, ReadSeekCloser, error)
	// WriteSHA1Object opens an object for writing to storage. The returned writer
	// must return an error on Close and discard the object if less than size
	// bytes were written.
	WriteSHA1Object(prefix object.Prefix) (WriteFinisher, error)
}

// ObjectDir is an ObjectReadWriter that stores objects on the local filesystem.
// It creates a directory tree similar to the .git/objects directory, but does
// not zlib-compress the files so that the files are seekable.
type ObjectDir string

func (dir ObjectDir) path(id githash.SHA1) string {
	return filepath.Join(string(dir), hex.EncodeToString(id[:1]), hex.EncodeToString(id[1:]))
}

type objectDirReader struct {
	*io.SectionReader
	io.Closer
}

// ReadObject opens an object from dir.
func (dir ObjectDir) ReadSHA1Object(id githash.SHA1) (prefix object.Prefix, obj ReadSeekCloser, err error) {
	f, err := os.Open(dir.path(id))
	if err != nil {
		return object.Prefix{}, nil, err
	}
	defer func() {
		if err != nil {
			f.Close()
		}
	}()

	// Read object prefix
	const maxTypeChars = len(object.TypeCommit)
	const maxSizeDigits = 20
	const maxPrefixLen = maxTypeChars + 1 + maxSizeDigits + 1
	buf := make([]byte, 0, maxPrefixLen)
	for bytes.IndexByte(buf, 0) == -1 && len(buf) < cap(buf) {
		n, err := f.Read(buf[len(buf):cap(buf)])
		if n == 0 && err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return object.Prefix{}, nil, fmt.Errorf("read object %v: %w", id, err)
		}
		buf = buf[:len(buf)+n]
	}

	sizeEnd := bytes.IndexByte(buf, 0)
	if sizeEnd == -1 {
		return object.Prefix{}, nil, fmt.Errorf("read object %v: missing object prefix", id)
	}
	prefixEnd := sizeEnd + 1
	if err := prefix.UnmarshalBinary(buf[:prefixEnd]); err != nil {
		return object.Prefix{}, nil, fmt.Errorf("read object %v: %w", id, err)
	}

	obj = objectDirReader{
		SectionReader: io.NewSectionReader(f, int64(prefixEnd), prefix.Size),
		Closer:        f,
	}
	return prefix, obj, nil
}

type objectDirWriter struct {
	f         *os.File
	dir       ObjectDir
	typ       object.Type
	sha1      hash.Hash
	remaining int64
	err       error
}

// WriteObject opens an object for writing into dir.
func (dir ObjectDir) WriteSHA1Object(prefix object.Prefix) (WriteFinisher, error) {
	f, err := ioutil.TempFile(string(dir), "object")
	if err != nil {
		return nil, fmt.Errorf("write %s: %w", prefix.Type, err)
	}
	defer func() {
		if err != nil {
			name := f.Name()
			f.Close()
			os.Remove(name)
		}
	}()
	h := sha1.New()
	prefixData, err := prefix.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("write %s: %w", prefix.Type, err)
	}
	h.Write(prefixData)
	if _, err := f.Write(prefixData); err != nil {
		return nil, fmt.Errorf("write %s: %w", prefix.Type, err)
	}
	return &objectDirWriter{
		f:         f,
		dir:       dir,
		typ:       prefix.Type,
		sha1:      h,
		remaining: prefix.Size,
	}, nil
}

func (w *objectDirWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if w.err != nil {
		return 0, w.err
	}
	if int64(len(p)) > w.remaining {
		p = p[:int(w.remaining)]
		w.err = fmt.Errorf("write %s: more bytes than expected", w.typ)
	}
	n, err := w.f.Write(p)
	w.remaining -= int64(n)
	w.sha1.Write(p[:n])
	if err == nil {
		err = w.err
	} else {
		err = fmt.Errorf("write %s: %w", w.typ, err)
	}
	return n, err
}

func (w *objectDirWriter) FinishObject() (_ []byte, err error) {
	name := w.f.Name()
	defer func() {
		if err != nil {
			os.Remove(name)
		}
	}()

	closeErr := w.f.Close()
	if w.err != nil {
		return nil, w.err
	}
	if w.remaining > 0 {
		// Not a complete object.
		return nil, fmt.Errorf("write %s: less bytes than expected (missing %d bytes)", w.typ, w.remaining)
	}
	var id githash.SHA1
	w.sha1.Sum(id[:0])
	if closeErr != nil {
		return nil, fmt.Errorf("write %s %v: %w", w.typ, id, closeErr)
	}
	dst := w.dir.path(id)
	// dir should exist, but intermediate directory might not.
	if err := os.MkdirAll(filepath.Dir(dst), 0o777); err != nil {
		return nil, fmt.Errorf("write %s %v: %w", w.typ, id, err)
	}
	if err := os.Rename(name, dst); err != nil {
		return nil, fmt.Errorf("write %s %v: %w", w.typ, id, err)
	}
	return id[:], nil
}
