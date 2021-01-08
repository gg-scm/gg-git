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

package client

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"gg-scm.io/pkg/git/githash"
	"gg-scm.io/pkg/git/internal/pktline"
	"gg-scm.io/pkg/git/packfile"
)

// PushStream represents a git-receive-pack session.
type PushStream struct {
	urlstr string
	refs   []*Ref
	conn   receivePackConn

	wroteCommands bool
	pw            *packfile.Writer
}

// StartPush starts a git-receive-pack session, reading the ref advertisements.
// The Context is used for the entire push stream.
func (r *Remote) StartPush(ctx context.Context) (_ *PushStream, err error) {
	conn, err := r.impl.receivePack(ctx)
	if err != nil {
		return nil, fmt.Errorf("push %s: %w", r.urlstr, err)
	}
	defer func() {
		if err != nil {
			conn.Close()
		}
	}()
	refs, err := readRefAdvertisementV1(pktline.NewReader(conn))
	if err != nil {
		return nil, fmt.Errorf("push %s: %w", r.urlstr, err)
	}
	return &PushStream{
		urlstr: r.urlstr,
		refs:   refs,
		conn:   conn,
	}, nil
}

func readRefAdvertisementV1(r *pktline.Reader) ([]*Ref, error) {
	// First line is a ref but also includes capabilities.
	var refs []*Ref
	r.Next()
	line, err := r.Text()
	if err != nil {
		return nil, fmt.Errorf("read refs: first ref: %w", err)
	}
	if bytes.Equal(line, []byte("version 1")) {
		// Skip optional initial "version 1" packet.
		r.Next()
		line, err = r.Text()
		if err != nil {
			return nil, fmt.Errorf("read refs: first ref: %w", err)
		}
	}
	ref0, _, err := parseFirstRefV1(line)
	if err != nil {
		return nil, fmt.Errorf("read refs: %w", err)
	}
	if ref0 == nil {
		// Expect flush next.
		// TODO(someday): Or shallow?
		if !r.Next() {
			return nil, fmt.Errorf("read refs: %w", r.Err())
		}
		if r.Type() != pktline.Flush {
			return nil, fmt.Errorf("read refs: expected flush after no-refs")
		}
		return nil, nil
	}
	refs = append(refs, ref0)

	// Subsequent lines are just refs.
	for r.Next() && r.Type() != pktline.Flush {
		line, err := r.Text()
		if err != nil {
			return nil, fmt.Errorf("read refs: %w", err)
		}
		ref, err := parseOtherRefV1(line)
		if err != nil {
			return nil, fmt.Errorf("read refs: %w", err)
		}
		refs = append(refs, ref)
	}
	if err := r.Err(); err != nil {
		return nil, fmt.Errorf("read refs: %w", err)
	}
	return refs, nil
}

func parseFirstRefV1(line []byte) (*Ref, []string, error) {
	refEnd := bytes.IndexByte(line, 0)
	if refEnd == -1 {
		return nil, nil, fmt.Errorf("first ref: missing nul")
	}
	idEnd := bytes.IndexByte(line[:refEnd], ' ')
	if idEnd == -1 {
		return nil, nil, fmt.Errorf("first ref: missing space")
	}
	id, err := githash.ParseSHA1(string(line[:idEnd]))
	if err != nil {
		return nil, nil, fmt.Errorf("first ref: %w", err)
	}
	refName := string(line[idEnd+1 : refEnd])
	caps := strings.Fields(string(line[refEnd+1:]))
	if refName == "capabilities^{}" {
		if id != (githash.SHA1{}) {
			return nil, nil, fmt.Errorf("first ref: non-zero ID passed with no-refs response")
		}
		return nil, caps, nil
	}
	return &Ref{
		ID:   id,
		Name: refName,
	}, caps, nil
}

func parseOtherRefV1(line []byte) (*Ref, error) {
	line = trimLF(line)
	idEnd := bytes.IndexByte(line, ' ')
	if idEnd == -1 {
		return nil, fmt.Errorf("ref: missing space")
	}
	refName := string(line[idEnd+1:])
	id, err := githash.ParseSHA1(string(line[:idEnd]))
	if err != nil {
		return nil, fmt.Errorf("ref: %s: %w", refName, err)
	}
	return &Ref{
		ID:   id,
		Name: refName,
	}, nil
}

// Refs returns the refs the remote sent when the stream started.
// The caller must not modify the returned slice.
func (p *PushStream) Refs() []*Ref {
	return p.refs
}

// A PushCommand is an instruction to update a remote ref.
type PushCommand struct {
	Old     githash.SHA1
	New     githash.SHA1
	RefName string
}

func (cmd *PushCommand) isDelete() bool {
	return cmd.New == githash.SHA1{}
}

// String returns the wire representation of the push command.
func (cmd *PushCommand) String() string {
	return cmd.Old.String() + " " + cmd.New.String() + " " + cmd.RefName
}

// WriteCommands informs the remote what ref changes to make once the stream is
// complete. This must be called at most once, and must be called before any
// calls to WriteHeader or Write.
func (p *PushStream) WriteCommands(objectCount uint32, commands ...*PushCommand) error {
	// Verify preconditions.
	if p.wroteCommands {
		return fmt.Errorf("push %s: WriteCommands called multiple times", p.urlstr)
	}
	if len(commands) == 0 {
		if objectCount > 0 {
			return fmt.Errorf("push %s: cannot write objects with no commands", p.urlstr)
		}
		return nil
	}
	hasNonDelete := false
	for _, c := range commands {
		if !c.isDelete() {
			hasNonDelete = true
		}
	}
	if objectCount > 0 && !hasNonDelete {
		return fmt.Errorf("push %s: cannot write objects without non-delete commands", p.urlstr)
	}

	// Write commands.
	p.wroteCommands = true
	var buf []byte
	for i, c := range commands {
		if i == 0 {
			buf = pktline.AppendString(buf, c.String()+"\x00report-status\n")
		} else {
			buf = pktline.AppendString(buf, c.String()+"\n")
		}
	}
	buf = pktline.AppendFlush(buf)
	if _, err := p.conn.Write(buf); err != nil {
		return fmt.Errorf("push %s: write commands: %w", p.urlstr, err)
	}
	if hasNonDelete {
		p.pw = packfile.NewWriter(p.conn, objectCount)
	}
	return nil
}

// WriteHeader writes hdr and prepares to accept the object's contents.
// WriteHeader returns an error if it is called before WriteCommands.
//
// See *packfile.Writer.WriteHeader for more details.
func (p *PushStream) WriteHeader(hdr *packfile.Header) (int64, error) {
	if p.pw == nil {
		return 0, fmt.Errorf("push %s: write header without relevant command", p.urlstr)
	}
	return p.pw.WriteHeader(hdr)
}

// Write writes to the current object in the packfile.
// Write returns an error if it is called before WriteCommands.
//
// See *packfile.Writer.Write for more details.
func (p *PushStream) Write(data []byte) (int, error) {
	if p.pw == nil {
		return 0, fmt.Errorf("push %s: write without relevant command", p.urlstr)
	}
	return p.pw.Write(data)
}

// Close completes the stream and releases any resources associated with the
// stream.
func (p *PushStream) Close() error {
	var err1 error
	if p.wroteCommands {
		err1 = p.readStatus()
	}
	err2 := p.conn.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

func (p *PushStream) readStatus() error {
	// Finish packfile, if one is being written.
	if p.pw != nil {
		if err := p.pw.Close(); err != nil {
			return fmt.Errorf("push %s: %w", p.urlstr, err)
		}
	}
	// Indicate that we're done writing. This is required for the HTTP protocol
	// to finish the request body.
	if err := p.conn.CloseWrite(); err != nil {
		return fmt.Errorf("push %s: %w", p.urlstr, err)
	}
	// Read status.
	report, err := readStatusReport(pktline.NewReader(p.conn))
	if err != nil {
		return fmt.Errorf("push %s: %w", p.urlstr, err)
	}
	if !report.isOK() {
		return report
	}
	return nil
}

type statusReport struct {
	status   string            // blank if ok
	commands map[string]string // ref name -> error mssage (blank if ok)
}

// readStatusReport reads a report-status from the wire.
// https://git-scm.com/docs/pack-protocol#_report_status
func readStatusReport(r *pktline.Reader) (*statusReport, error) {
	// Read unpack-status
	r.Next()
	line, err := r.Text()
	if err != nil {
		return nil, fmt.Errorf("read status report: %w", err)
	}
	const unpackStatusPrefix = "unpack "
	if !bytes.HasPrefix(line, []byte(unpackStatusPrefix)) {
		return nil, fmt.Errorf("read status report: did not start with %q", unpackStatusPrefix)
	}
	report := &statusReport{
		status:   string(trimLF(line[len(unpackStatusPrefix):])),
		commands: make(map[string]string),
	}
	if report.status == "" {
		return nil, fmt.Errorf("read status report: unpack status blank")
	}
	if report.status == "ok" {
		report.status = ""
	}

	// Read zero or more command-status packets.
	// Slightly more relaxed than spec.
	successPrefix := []byte("ok ")
	errorPrefix := []byte("ng ")
	for r.Next() && r.Type() != pktline.Flush {
		line, err := r.Text()
		if err != nil {
			return nil, fmt.Errorf("read status report: commands: %w", err)
		}
		switch {
		case bytes.HasPrefix(line, successPrefix):
			refName := string(line[len(successPrefix):])
			report.commands[refName] = ""
		case bytes.HasPrefix(line, errorPrefix):
			refNameAndError := string(line[len(errorPrefix):])
			i := strings.IndexByte(refNameAndError, ' ')
			if i == -1 || i == len(refNameAndError)-1 {
				return nil, fmt.Errorf("read status report: commands: missing error message for %q", refNameAndError)
			}
			refName := refNameAndError[:i]
			errorMsg := refNameAndError[i+1:]
			report.commands[refName] = errorMsg
		default:
			return nil, fmt.Errorf("read status report: commands: unknown command status")
		}
	}
	if err := r.Err(); err != nil {
		return nil, fmt.Errorf("read status report: %w", err)
	}
	return report, nil
}

func (report *statusReport) isOK() bool {
	if report.status != "" {
		return false
	}
	for _, msg := range report.commands {
		if msg != "" {
			return false
		}
	}
	return true
}

func (report *statusReport) Error() string {
	if report.status == "" {
		return "remote: could not update refs"
	}
	return "remote: " + report.status
}
