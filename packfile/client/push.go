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
)

// receive-pack capability names.
// See https://git-scm.com/docs/protocol-capabilities
const (
	deleteRefsCap   = "delete-refs"
	reportStatusCap = "report-status"
)

// PushStream represents a git-receive-pack session.
type PushStream struct {
	urlstr string
	refs   map[githash.Ref]*Ref
	caps   capabilityList
	conn   receivePackConn

	wroteCommands bool
	hasPack       bool
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
	connReader := pktline.NewReader(conn)
	connReader.Next()
	ref0, caps, err := readFirstRefV1(connReader)
	if err != nil {
		return nil, fmt.Errorf("push %s: %w", r.urlstr, err)
	}
	var refs map[githash.Ref]*Ref
	if ref0 != nil {
		refs = map[githash.Ref]*Ref{
			ref0.Name: ref0,
		}
		if err := readOtherRefsV1(refs, caps.symrefs(), connReader); err != nil {
			return nil, fmt.Errorf("push %s: %w", r.urlstr, err)
		}
	}
	return &PushStream{
		urlstr: r.urlstr,
		refs:   refs,
		caps:   caps,
		conn:   conn,
	}, nil
}

// Refs returns the refs the remote sent when the stream started.
// The caller must not modify the returned map.
func (p *PushStream) Refs() map[githash.Ref]*Ref {
	return p.refs
}

// A PushCommand is an instruction to update a remote ref. At least one of Old
// or New must be set.
type PushCommand struct {
	RefName githash.Ref
	Old     githash.SHA1 // if not set, then create the ref
	New     githash.SHA1 // if not set, then delete the ref
}

func (cmd *PushCommand) isZero() bool {
	return cmd.New == githash.SHA1{} && cmd.Old == githash.SHA1{}
}

func (cmd *PushCommand) isDelete() bool {
	return cmd.New == githash.SHA1{} && cmd.Old != githash.SHA1{}
}

// String returns the wire representation of the push command.
func (cmd *PushCommand) String() string {
	return cmd.Old.String() + " " + cmd.New.String() + " " + cmd.RefName.String()
}

// WriteCommands informs the remote what ref changes to make once the stream is
// complete. This must be called at most once, and must be called before any
// calls to Write.
func (p *PushStream) WriteCommands(commands ...*PushCommand) error {
	// Verify preconditions.
	if p.wroteCommands {
		return fmt.Errorf("push %s: WriteCommands called multiple times", p.urlstr)
	}
	if len(commands) == 0 {
		return nil
	}

	// Determine which capabilities we can use.
	useCaps := capabilityList{
		reportStatusCap: "",
		ofsDeltaCap:     "",
		deleteRefsCap:   "",
	}
	useCaps.intersect(p.caps)
	hasNonDelete := false
	for _, c := range commands {
		if c.isZero() {
			return fmt.Errorf("push %s: empty command for %s", p.urlstr, c.RefName)
		}
		if c.isDelete() {
			if !p.caps.supports(deleteRefsCap) {
				return fmt.Errorf("push %s: remote does not support deleting refs", p.urlstr)
			}
		} else {
			hasNonDelete = true
		}
	}

	// Write commands.
	p.wroteCommands = true
	var buf []byte
	for i, c := range commands {
		if i == 0 {
			buf = pktline.AppendString(buf, c.String()+"\x00"+useCaps.String()+"\n")
		} else {
			buf = pktline.AppendString(buf, c.String()+"\n")
		}
	}
	buf = pktline.AppendFlush(buf)
	if _, err := p.conn.Write(buf); err != nil {
		return fmt.Errorf("push %s: write commands: %w", p.urlstr, err)
	}
	p.hasPack = hasNonDelete
	return nil
}

// Write writes packfile data. It returns an error if it is called before
// WriteCommands.
func (p *PushStream) Write(data []byte) (int, error) {
	if !p.hasPack {
		return 0, fmt.Errorf("push %s: write without relevant command", p.urlstr)
	}
	return p.conn.Write(data)
}

// Close completes the stream and releases any resources associated with the
// stream.
func (p *PushStream) Close() error {
	var err1 error
	if p.wroteCommands && p.caps.supports(reportStatusCap) {
		err1 = p.readStatus()
	}
	err2 := p.conn.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

func (p *PushStream) readStatus() error {
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
		status:   string(line[len(unpackStatusPrefix):]),
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
