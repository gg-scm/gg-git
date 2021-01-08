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

package pktline

import (
	"strings"
	"testing"
)

func TestReadPacketLine(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		typ     Type
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
			typ:   Data,
			want:  "a\n",
		},
		{
			name:  "TextNoLF",
			input: "0005a",
			typ:   Data,
			want:  "a",
		},
		{
			name:  "LongerString",
			input: "000bfoobar\n",
			typ:   Data,
			want:  "foobar\n",
		},
		{
			name:  "Empty",
			input: "0004",
			typ:   Data,
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
			typ:   Flush,
		},
		{
			name:  "Delim",
			input: "0001",
			typ:   Delim,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := NewReader(strings.NewReader(test.input))
			r.Next()
			if err := r.Err(); err != nil {
				if !test.wantErr {
					t.Errorf("r.Err() = %v", err)
				}
				return
			}
			gotType := r.Type()
			got, bytesErr := r.Bytes()
			if test.wantErr {
				t.Fatalf("r.Type()=%v r.Bytes()=%q,%v r.Err()=<nil>; want error", gotType, got, bytesErr)
			}
			if gotType != test.typ || (test.typ == Data && bytesErr != nil) || string(got) != test.want {
				t.Errorf("r.Type()=%v r.Bytes()=%q,%v; want r.Type()=%v r.Bytes()=%q,...", gotType, got, bytesErr, test.typ, test.want)
			}
		})
	}
}
