// Copyright 2020 The gg Authors
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

package object

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"io"
	"strings"
	"time"

	"gg-scm.io/pkg/git/githash"
)

/*
Unfortunately, the tag object is the least documented of all the Git objects.

Reference parser: https://github.com/git/git/blob/6da43d937ca96d277556fa92c5a664fb1cbcc8ac/tag.c#L134-L206

It appears as though tag signatures are encoded as ASCII-armored GPG detached
signatures appended to the message: https://github.com/git/git/blob/21bf933928c02372633b88aa6c4d9d71271d42b3/builtin/tag.c#L129-L132
*/

// A Tag is a parsed Git tag object. These are referred to as "annotated tags"
// in the Git documentation.
type Tag struct {
	// ObjectID is the hash of the object that the tag refers to.
	ObjectID githash.SHA1
	// ObjectType is the type of the object that the tag refers to.
	ObjectType Type

	// Name is the name of the tag.
	Name string

	// Tagger identifies the person who created the tag.
	Tagger User
	// Time is the time the tag was created.
	// The Location is significant.
	Time time.Time

	// Message is the tag message.
	Message string
}

// ParseTag deserializes a tag in the Git object format. It is the same as
// calling UnmarshalText on a new tag.
func ParseTag(data []byte) (*Tag, error) {
	t := new(Tag)
	err := t.UnmarshalText(data)
	return t, err
}

// UnmarshalText deserializes a tag from the Git object format.
func (t *Tag) UnmarshalText(data []byte) error {
	var ok bool
	data, ok = consumeString(data, "object ")
	if !ok {
		return fmt.Errorf("parse git tag: object: missing")
	}
	*t = Tag{}
	var err error
	data, err = consumeHex(t.ObjectID[:], data)
	if err != nil {
		return fmt.Errorf("parse git tag: object: %w", err)
	}
	data, ok = consumeString(data, "\n")
	if !ok {
		return fmt.Errorf("parse git tag: object: trailing data")
	}

	data, ok = consumeString(data, "type ")
	if !ok {
		return fmt.Errorf("parse git tag: type: missing line")
	}
	typ, data, err := consumeLine(data)
	if err != nil {
		return fmt.Errorf("parse git tag: type: %w", err)
	}
	t.ObjectType = Type(typ)
	if !t.ObjectType.IsValid() {
		return fmt.Errorf("parse git tag: type: %q invalid", t.ObjectType)
	}

	data, ok = consumeString(data, "tag ")
	if !ok {
		return fmt.Errorf("parse git tag: name: missing line")
	}
	t.Name, data, err = consumeLine(data)
	if err != nil {
		return fmt.Errorf("parse git tag: name: %w", err)
	}

	data, ok = consumeString(data, "tagger ")
	if !ok {
		return fmt.Errorf("parse git tag: tagger: missing line")
	}
	t.Tagger, t.Time, data, err = consumeUser(data)
	if err != nil {
		return fmt.Errorf("parse git tag: tagger: %w", err)
	}

	data, ok = consumeString(data, "\n")
	if !ok {
		return fmt.Errorf("parse git tag: message: expect blank line after header")
	}
	t.Message = string(data)
	return nil
}

func consumeLine(src []byte) (_ string, tail []byte, _ error) {
	eol := bytes.IndexByte(src, '\n')
	if eol == -1 {
		return "", src, io.ErrUnexpectedEOF
	}
	return string(src[:eol]), src[eol+1:], nil
}

// MarshalText serializes a tag into the Git object format.
func (t *Tag) MarshalText() ([]byte, error) {
	buf := new(bytes.Buffer)
	fmt.Fprintf(buf, "object %x\n", t.ObjectID)
	if !t.ObjectType.IsValid() {
		return nil, fmt.Errorf("marshal git tag: invalid object type %q", t.ObjectType)
	}
	fmt.Fprintf(buf, "type %v\n", t.ObjectType)
	if !isSafeForHeader(t.Name) {
		return nil, fmt.Errorf("marshal git tag: name %q contains unsafe characters", t.Name)
	}
	fmt.Fprintf(buf, "tag %s\n", t.Name)
	if err := writeUser(buf, "tagger", t.Tagger, t.Time); err != nil {
		return nil, fmt.Errorf("marshal git tag: %w", err)
	}
	buf.WriteString("\n")
	buf.WriteString(t.Message)
	return buf.Bytes(), nil
}

// SHA1 computes the SHA-1 hash of the tag object.
func (t *Tag) SHA1() githash.SHA1 {
	h := sha1.New()
	s, err := t.MarshalText()
	if err != nil {
		panic(err)
	}
	h.Write(AppendPrefix(nil, TypeTag, int64(len(s))))
	h.Write(s)
	var arr githash.SHA1
	h.Sum(arr[:0])
	return arr
}

// Summary returns the first line of the message.
func (t *Tag) Summary() string {
	i := strings.IndexByte(t.Message, '\n')
	if i == -1 {
		return t.Message
	}
	return t.Message[:i]
}
