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
	"encoding/hex"
	"fmt"
	"io"
)

const maxPacketSize = 65520

type packetType int8

const (
	flushPacket packetType = 0
	delimPacket packetType = 1
	dataPacket  packetType = 4
)

func readPacketLine(r io.Reader, p []byte) (packetType, int, error) {
	var lengthHex [4]byte
	if _, err := io.ReadFull(r, lengthHex[:]); err != nil {
		return 0, 0, err
	}
	var length [2]byte
	if _, err := hex.Decode(length[:], lengthHex[:]); err != nil {
		return 0, 0, err
	}
	switch {
	case length[0] == 0 && length[1] == 0:
		return flushPacket, 0, nil
	case length[0] == 0 && length[1] == 1:
		return delimPacket, 0, nil
	case length[0] == 0 && length[1] < byte(len(lengthHex)):
		return 0, 0, fmt.Errorf("read packet line: invalid length %q", lengthHex)
	}
	n := int(length[0])<<8 | int(length[1]) - len(lengthHex)
	if n == 0 {
		return dataPacket, 0, nil
	}
	if n+len(lengthHex) > maxPacketSize {
		return 0, 0, fmt.Errorf("read packet line: invalid length %q", lengthHex)
	}
	if n > len(p) {
		return 0, 0, fmt.Errorf("read packet line: buffer too small")
	}
	nr, err := io.ReadFull(r, p[:n])
	return dataPacket, nr, err
}

func trimLF(line []byte) []byte {
	if len(line) == 0 || line[len(line)-1] != '\n' {
		return line
	}
	return line[:len(line)-1]
}

func appendPacketLine(dst []byte, line []byte) []byte {
	n := len(line) + 4
	dst = append(dst,
		hexDigits[n>>12],
		hexDigits[n>>8&0xf],
		hexDigits[n>>4&0xf],
		hexDigits[n&0xf],
	)
	dst = append(dst, line...)
	return dst
}

func appendPacketLineString(dst []byte, line string) []byte {
	n := len(line) + 4
	dst = append(dst,
		hexDigits[n>>12],
		hexDigits[n>>8&0xf],
		hexDigits[n>>4&0xf],
		hexDigits[n&0xf],
	)
	dst = append(dst, line...)
	return dst
}

func appendFlushPacket(dst []byte) []byte {
	return append(dst, "0000"...)
}

func appendDelimPacket(dst []byte) []byte {
	return append(dst, "0001"...)
}

const hexDigits = "0123456789abcdef"
