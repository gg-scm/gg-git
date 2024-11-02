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
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"gg-scm.io/pkg/git/githash"
)

// A Commit is a parsed Git commit object.
type Commit struct {
	// Tree is the hash of the commit's tree object.
	Tree githash.SHA1
	// Parents are the hashes of the commit's parents.
	Parents []githash.SHA1

	// Author identifies the person who wrote the code.
	Author User
	// AuthorTime is the time the code was written.
	// The Location is significant.
	AuthorTime time.Time

	// Committer identifies the person who committed the code to the repository.
	Committer User
	// CommitTime is the time the code was committed to the repository.
	// The Location is significant.
	CommitTime time.Time

	// Extra stores any lines in the commit object
	// between the committer line and the gpgsig line.
	// It will never begin or end with a newline,
	// nor will it contain blank lines.
	Extra CommitFields

	// If GPGSignature is not empty, then it is the ASCII-armored signature of
	// the commit.
	GPGSignature []byte

	// Message is the commit message.
	Message string
}

// ParseCommit deserializes a commit in the Git object format. It is the same as
// calling UnmarshalText on a new commit.
func ParseCommit(data []byte) (*Commit, error) {
	c := new(Commit)
	err := c.UnmarshalText(data)
	return c, err
}

// UnmarshalText deserializes a commit from the Git object format. It is the
// same as calling UnmarshalBinary.
func (c *Commit) UnmarshalText(data []byte) error {
	return c.UnmarshalBinary(data)
}

// UnmarshalBinary deserializes a commit from the Git object format.
func (c *Commit) UnmarshalBinary(data []byte) error {
	// See parse_commit_buffer in Git's commit.c for accepted format.
	// It's pretty loose, but the first 4 keys must be in this order:
	// 1. tree
	// 2. parent (zero or more)
	// 3. author
	// 4. committer
	//
	// The only other restriction AFAICT is
	// gpgsig must be immediately before the message.

	var ok bool
	data, ok = consumeString(data, "tree ")
	if !ok {
		return fmt.Errorf("parse git commit: tree: missing")
	}
	*c = Commit{}
	var err error
	data, err = consumeHex(c.Tree[:], data)
	if err != nil {
		return fmt.Errorf("parse git commit: tree: %w", err)
	}
	data, ok = consumeString(data, "\n")
	if !ok {
		return fmt.Errorf("parse git commit: tree: trailing data")
	}
	for i := 0; ; i++ {
		data, ok = consumeString(data, "parent ")
		if !ok {
			break
		}
		var p githash.SHA1
		data, err = consumeHex(p[:], data)
		if err != nil {
			return fmt.Errorf("parse git commit: parent %d: %w", i, err)
		}
		c.Parents = append(c.Parents, p)
		data, ok = consumeString(data, "\n")
		if !ok {
			return fmt.Errorf("parse git commit: parent %d: trailing data", i)
		}
	}
	data, ok = consumeString(data, "author ")
	if !ok {
		return fmt.Errorf("parse git commit: author: missing line")
	}
	c.Author, c.AuthorTime, data, err = consumeUser(data)
	if err != nil {
		return fmt.Errorf("parse git commit: author: %w", err)
	}
	data, ok = consumeString(data, "committer ")
	if !ok {
		return fmt.Errorf("parse git commit: committer: missing line")
	}
	c.Committer, c.CommitTime, data, err = consumeUser(data)
	if err != nil {
		return fmt.Errorf("parse git commit: committer: %w", err)
	}
	extra := new(strings.Builder)
	for {
		data, ok = consumeString(data, "gpgsig ")
		if ok {
			c.GPGSignature, data, err = consumeSignature(data)
			if err != nil {
				return fmt.Errorf("parse git commit: gpg signature: %w", err)
			}
			break
		}
		eol := bytes.IndexByte(data, '\n')
		if eol == 0 {
			break
		}
		if eol == -1 {
			return fmt.Errorf("parse git commit: message: expect blank line after header")
		}
		extra.Write(data[:eol+1])
		data = data[eol+1:]
	}
	c.Extra = CommitFields(strings.TrimSuffix(extra.String(), "\n"))
	data, ok = consumeString(data, "\n")
	if !ok {
		return fmt.Errorf("parse git commit: message: expect blank line after header")
	}
	c.Message = string(data)
	return nil
}

// MarshalText serializes a commit into the Git object format. It is the same as
// calling MarshalBinary.
func (c *Commit) MarshalText() ([]byte, error) {
	return c.MarshalBinary()
}

// MarshalBinary serializes a commit into the Git object format.
func (c *Commit) MarshalBinary() ([]byte, error) {
	// See commit_tree_extended in Git's commit.c for equivalent logic.

	buf := new(bytes.Buffer)
	fmt.Fprintf(buf, "tree %x\n", c.Tree)
	for _, par := range c.Parents {
		fmt.Fprintf(buf, "parent %x\n", par)
	}
	if err := writeUser(buf, "author", c.Author, c.AuthorTime); err != nil {
		return nil, fmt.Errorf("marshal git commit: %w", err)
	}
	if err := writeUser(buf, "committer", c.Committer, c.CommitTime); err != nil {
		return nil, fmt.Errorf("marshal git commit: %w", err)
	}
	if !c.Extra.IsValid() {
		return nil, fmt.Errorf("marshal git commit: extra headers not valid")
	}
	if len(c.Extra) > 0 {
		buf.WriteString(string(c.Extra))
		buf.WriteString("\n")
	}
	if err := writeGPGSignature(buf, c.GPGSignature); err != nil {
		return nil, fmt.Errorf("marshal git commit: %w", err)
	}
	buf.WriteString("\n")
	buf.WriteString(c.Message)
	return buf.Bytes(), nil
}

func writeUser(w io.Writer, name string, u User, t time.Time) error {
	if !isSafeForHeader(string(u)) {
		return fmt.Errorf("%s: %q contains unsafe characters", name, u)
	}
	_, err := fmt.Fprintf(w, "%s %s %d %s\n", name, u, t.Unix(), tzOffset(t))
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func tzOffset(t time.Time) string {
	name, offset := t.Zone()
	// If the zone name is usable as a timezone offset, use it verbatim.
	// This is critical for round-tripping commits with non-standard timezones.
	if got, err := parseTZOffset([]byte(name)); err == nil && offset == got {
		return name
	}
	return t.Format("-0700")
}

// SHA1 computes the SHA-1 hash of the commit object. This is commonly known as
// the "commit hash" and uniquely identifies the commit.
func (c *Commit) SHA1() githash.SHA1 {
	h := sha1.New()
	s, err := c.MarshalText()
	if err != nil {
		panic(err)
	}
	h.Write(AppendPrefix(nil, TypeCommit, int64(len(s))))
	h.Write(s)
	var arr githash.SHA1
	h.Sum(arr[:0])
	return arr
}

// Summary returns the first line of the message.
func (c *Commit) Summary() string {
	i := strings.IndexByte(c.Message, '\n')
	if i == -1 {
		return c.Message
	}
	return c.Message[:i]
}

func consumeString(src []byte, s string) (_ []byte, ok bool) {
	if len(src) < len(s) {
		return src, false
	}
	for i := 0; i < len(s); i++ {
		if src[i] != s[i] {
			return src, false
		}
	}
	return src[len(s):], true
}

func consumeHex(dst []byte, src []byte) (tail []byte, _ error) {
	n := hex.EncodedLen(len(dst))
	if len(src) < n {
		return src, io.ErrUnexpectedEOF
	}
	if _, err := hex.Decode(dst, src[:n]); err != nil {
		return src, err
	}
	return src[n:], nil
}

func consumeUser(src []byte) (_ User, _ time.Time, tail []byte, _ error) {
	eol := bytes.IndexByte(src, '\n')
	if eol == -1 {
		return "", time.Time{}, src, io.ErrUnexpectedEOF
	}
	line := src[:eol]
	tail = src[eol+1:]

	// Find landmarks from end of line, since we don't want to make assumptions
	// about the user field.
	timestampEnd := bytes.LastIndexByte(line, ' ')
	if timestampEnd == -1 {
		return "", time.Time{}, src, fmt.Errorf("invalid format")
	}
	tzOffsetStart := timestampEnd + 1
	userEnd := bytes.LastIndexByte(line[:timestampEnd], ' ')
	if userEnd == -1 {
		return "", time.Time{}, src, fmt.Errorf("invalid format")
	}
	timestampStart := userEnd + 1

	timestamp, err := strconv.ParseInt(string(line[timestampStart:timestampEnd]), 10, 64)
	if err != nil {
		return "", time.Time{}, src, fmt.Errorf("parse timestamp: %w", err)
	}
	tzPart := line[tzOffsetStart:]
	tzOffset, err := parseTZOffset(tzPart)
	if err != nil {
		return "", time.Time{}, src, err
	}
	tz := time.FixedZone(string(tzPart), tzOffset)
	return User(line[:userEnd]), time.Unix(timestamp, 0).In(tz), tail, nil
}

func consumeSignature(src []byte) (sig, tail []byte, _ error) {
	// Consume rest of first line (gpgsig line).
	i := bytes.IndexByte(src, '\n')
	if i == -1 {
		return nil, src, fmt.Errorf("parse signature: %w", io.ErrUnexpectedEOF)
	}
	sig = append(sig, src[:i+1]...)
	tail = src[i+1:]

	// Subsequent lines must start with a space.
	for len(tail) > 0 && tail[0] == ' ' {
		i := bytes.IndexByte(tail, '\n')
		if i == -1 {
			return sig, tail, fmt.Errorf("parse signature: %w", io.ErrUnexpectedEOF)
		}
		sig = append(sig, tail[1:i+1]...)
		tail = tail[i+1:]
	}
	return sig, tail, nil
}

func parseTZOffset(src []byte) (int, error) {
	if len(src) < 2 || len(src) > 5 {
		return 0, fmt.Errorf("parse UTC offset %q: wrong length", src)
	}
	var sign int
	switch src[0] {
	case '-':
		sign = -1
	case '+':
		sign = 1
	default:
		return 0, fmt.Errorf("parse UTC offset %q: must start with plus or minus sign", src)
	}
	digits := src[1:]
	for _, b := range digits {
		if b < '0' || b > '9' {
			return 0, fmt.Errorf("parse UTC offset %q: must only have digits after sign", src)
		}
	}
	hours := tzDigit(digits, -4)*10 + tzDigit(digits, -3)
	minutes := tzDigit(digits, -2)*10 + tzDigit(digits, -1)
	return (hours*60*60 + minutes*60) * sign, nil
}

func tzDigit(digits []byte, i int) int {
	i = len(digits) + i
	if i < 0 {
		return 0
	}
	return int(digits[i] - '0')
}

var gpgSignaturePrefix = []byte("gpgsig")

func writeGPGSignature(w io.Writer, sig []byte) error {
	if len(sig) == 0 {
		return nil
	}
	if _, err := w.Write(gpgSignaturePrefix); err != nil {
		return fmt.Errorf("write gpg signature: %w", err)
	}
	sp := []byte(" ")
	for len(sig) > 0 {
		lineEnd := bytes.IndexByte(sig, '\n')
		if lineEnd == -1 {
			return fmt.Errorf("write gpg signature: data has unterminated line")
		}
		if _, err := w.Write(sp); err != nil {
			return fmt.Errorf("write gpg signature: %w", err)
		}
		if _, err := w.Write(sig[:lineEnd+1]); err != nil {
			return fmt.Errorf("write gpg signature: %w", err)
		}
		sig = sig[lineEnd+1:]
	}
	return nil
}

// User identifies an author or committer as a string like
// "Octocat <octocat@example.com>".
type User string

// MakeUser constructs a User string from a name and an email address.
func MakeUser(name, email string) (User, error) {
	if name != strings.TrimSpace(name) {
		return "", fmt.Errorf("make user: name %q has surrounding whitespace", name)
	}
	if strings.Contains(name, "<") {
		return "", fmt.Errorf("make user: name %q contains '<'", name)
	}
	if !isSafeForHeader(name) {
		return "", fmt.Errorf("make user: name %q contains unsafe characters", name)
	}
	if strings.Contains(email, ">") {
		return "", fmt.Errorf("make user: email %q contains '>'", email)
	}
	if !isSafeForHeader(email) {
		return "", fmt.Errorf("make user: email %q contains unsafe characters", name)
	}
	if name == "" {
		return User("<" + email + ">"), nil
	}
	return User(name + " <" + email + ">"), nil
}

func (u User) split() (name, email string) {
	// Reference implementation is split_ident_line:
	// https://github.com/git/git/blob/9c31b19dd00981fcea435de1cd05eab179039a8d/ident.c#L271-L346

	nameEnd := strings.IndexByte(string(u), '<')
	if nameEnd == -1 {
		return strings.TrimSpace(string(u)), ""
	}
	emailStart := nameEnd + 1
	emailEnd := strings.IndexByte(string(u[emailStart:]), '>')
	if emailEnd == -1 {
		return strings.TrimSpace(string(u)), ""
	}
	emailEnd += emailStart
	name = strings.TrimSpace(string(u[:nameEnd]))
	email = string(u[emailStart:emailEnd])
	return
}

// Name returns the user's name.
func (u User) Name() string {
	name, _ := u.split()
	return name
}

// Email returns the user's email address, if they have one.
func (u User) Email() string {
	_, email := u.split()
	return email
}

// CommitFields is a block of lines, conventionally in the form "key value".
// A value may span multiple lines by starting each continuation line with a space.
type CommitFields string

// IsValid reports whether fields can be serialized into a Git commit.
// Valid fields do not begin or end with a newline
// and do not contain blank lines.
func (fields CommitFields) IsValid() bool {
	return !strings.HasPrefix(string(fields), "\n") &&
		!strings.HasSuffix(string(fields), "\n") &&
		!strings.Contains(string(fields), "\n\n") &&
		!strings.Contains(string(fields), "\x00")
}

// Cut slices fields around the first field.
func (fields CommitFields) Cut() (head, tail CommitFields) {
	for i := 0; ; {
		eol := strings.IndexByte(string(fields[i:]), '\n')
		if eol == -1 {
			return fields, ""
		}
		eol += i
		if !strings.HasPrefix(string(fields[eol+1:]), " ") {
			return fields[:eol], fields[eol+1:]
		}
		// Continuation line: keep going.
		i = eol + 1
	}
}

// First returns the first field's key and value.
func (fields CommitFields) First() (key, value string) {
	field, _ := fields.Cut()
	key, value = field.cutKV()
	value = normalizeContinuations(value)
	return
}

// cutKV cuts a single field into its key and value.
// field must only contain a single field.
func (field CommitFields) cutKV() (key, value string) {
	firstLine := string(field)
	if eol := strings.IndexByte(firstLine, '\n'); eol != -1 {
		firstLine = firstLine[:eol]
	}
	if sp := strings.IndexByte(firstLine, ' '); sp != -1 {
		return firstLine[:sp], string(field[sp+1:])
	}
	return firstLine, string(field[len(firstLine):])
}

// Get returns the value of the first field in fields with the given key,
// or the empty string if no such field exists.
func (fields CommitFields) Get(key string) string {
	for fields != "" {
		head, tail := fields.Cut()
		k, v := head.cutKV()
		if k == key {
			return normalizeContinuations(v)
		}
		fields = tail
	}
	return ""
}

func normalizeContinuations(s string) string {
	return strings.ReplaceAll(s, "\n ", "\n")
}

// isSafeForHeader reports whether s is safe to be included as an element of an
// object header.
func isSafeForHeader(s string) bool {
	return !strings.ContainsAny(s, "\x00\n")
}
