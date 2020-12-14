// Copyright 2020 The gg Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package githash

import (
	"bytes"
	"encoding"
	"fmt"
	"strings"
	"testing"
)

// Verify that SHA1 implements the various encoding interfaces.
var (
	_ fmt.Stringer               = SHA1{}
	_ fmt.Formatter              = SHA1{}
	_ encoding.TextMarshaler     = SHA1{}
	_ encoding.TextUnmarshaler   = &SHA1{}
	_ encoding.BinaryMarshaler   = SHA1{}
	_ encoding.BinaryUnmarshaler = &SHA1{}
)

func TestSHA1(t *testing.T) {
	tests := []struct {
		h     SHA1
		s     string
		short string
	}{
		{
			h:     SHA1{},
			s:     "0000000000000000000000000000000000000000",
			short: "00000000",
		},
		{
			h: SHA1{
				0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
				0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
				0x01, 0x23, 0x45, 0x67,
			},
			s:     "0123456789abcdef0123456789abcdef01234567",
			short: "01234567",
		},
	}
	for _, test := range tests {
		if got := test.h.String(); got != test.s {
			t.Errorf("SHA1(%x).String() = %q; want %q", test.h[:], got, test.s)
		}
		if got := test.h.Short(); got != test.short {
			t.Errorf("SHA1(%x).Short() = %q; want %q", test.h[:], got, test.short)
		}
		if got, err := test.h.MarshalText(); err != nil || string(got) != test.s {
			t.Errorf("SHA1(%x).MarshalText() = %q, %v; want %q, <nil>", test.h[:], got, err, test.s)
		}
		if got, err := test.h.MarshalBinary(); err != nil || !bytes.Equal(got, test.h[:]) {
			t.Errorf("SHA1(%x).MarshalBinary() = %#v, %v; want %#v, <nil>", test.h[:], got, err, test.h[:])
		}
	}

	t.Run("Format", func(t *testing.T) {
		for _, test := range tests {
			// Don't want to overspecify this, but is nice to see the output.
			t.Logf("%%#v = %#v", test.h)

			formatTests := []struct {
				format string
				want   string
			}{
				{"%x", test.s},
				{"%.4x", test.s[:8]},
				{"%#x", "0x" + test.s},
				{"%X", strings.ToUpper(test.s)},
				{"%#X", "0X" + strings.ToUpper(test.s)},
				{"%s", test.s},
				{"%v", test.s},
			}
			for _, ftest := range formatTests {
				if got := fmt.Sprintf(ftest.format, test.h); got != ftest.want {
					t.Errorf("fmt.Sprintf(%q, %x) = %q; want %q", ftest.format, test.h[:], got, ftest.want)
				}
			}
		}
	})
}

func TestParseSHA1(t *testing.T) {
	tests := []struct {
		s       string
		want    SHA1
		wantErr bool
	}{
		{s: "", wantErr: true},
		{s: "0000000000000000000000000000000000000000", want: SHA1{}},
		{
			s: "0123456789abcdef0123456789abcdef01234567",
			want: SHA1{
				0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
				0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
				0x01, 0x23, 0x45, 0x67,
			},
		},
		{
			s:       "0123456789abcdef0123456789abcdef0123456",
			wantErr: true,
		},
		{
			s:       "0123456789abcdef0123456789abcdef012345678",
			wantErr: true,
		},
		{
			s:       "01234567",
			wantErr: true,
		},
		{
			s:       "fooooooooooooooooooooooooooooooooooooooo",
			wantErr: true,
		},
	}
	for _, test := range tests {
		switch got, err := ParseSHA1(test.s); {
		case err == nil && !test.wantErr && got != test.want:
			t.Errorf("ParseSHA1(%q) = %v, <nil>; want %v, <nil>", test.s, got, test.want)
		case err == nil && test.wantErr:
			t.Errorf("ParseSHA1(%q) = %v, <nil>; want error", test.s, got)
		case err != nil && !test.wantErr:
			t.Errorf("ParseSHA1(%q) = _, %v; want %v, <nil>", test.s, err, test.want)
		}
	}
}
