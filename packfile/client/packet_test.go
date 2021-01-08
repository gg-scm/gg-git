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
	"strings"
	"testing"
)

func TestReadPacketLine(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		typ     packetType
		want    string
		wantErr bool
	}{
		{
			name:    "EOF",
			input:   "",
			wantErr: true,
		},
		{
			name:    "PartialLength",
			input:   "000",
			wantErr: true,
		},
		{
			name:  "TextLF",
			input: "0006a\n",
			typ:   dataPacket,
			want:  "a\n",
		},
		{
			name:  "TextNoLF",
			input: "0005a",
			typ:   dataPacket,
			want:  "a",
		},
		{
			name:  "LongerString",
			input: "000bfoobar\n",
			typ:   dataPacket,
			want:  "foobar\n",
		},
		{
			name:  "Empty",
			input: "0004",
			typ:   dataPacket,
			want:  "",
		},
		{
			name:    "ShortLength3",
			input:   "0003",
			wantErr: true,
		},
		{
			name:    "ShortLength2",
			input:   "0002",
			wantErr: true,
		},
		{
			name:  "Flush",
			input: "0000",
			typ:   flushPacket,
		},
		{
			name:  "Delim",
			input: "0001",
			typ:   delimPacket,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := make([]byte, maxPacketSize)
			gotType, gotN, err := readPacketLine(strings.NewReader(test.input), p)
			if err != nil {
				if !test.wantErr {
					t.Errorf("readPacketLine(...): %v", err)
				}
				return
			}
			got := string(p[:gotN])
			if test.wantErr {
				t.Fatalf("readPacketLine(...) typ=%v line=%q err=<nil>; want error", gotType, got)
			}
			if gotType != test.typ || got != test.want {
				t.Errorf("readPacketLine(...) typ=%v line=%q; want typ=%v line=%q", gotType, got, test.typ, test.want)
			}
		})
	}
}
