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

package git

import (
	"errors"
	"fmt"
	"os/exec"
	"testing"
)

func TestExitCode(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{
			err:  fakeExitError(1),
			want: 1,
		},
		{
			err:  fakeExitError(-1),
			want: -1,
		},
		{
			err:  fakeExitError(0),
			want: 0,
		},
		{
			err:  errors.New("bork"),
			want: -1,
		},
		{
			err:  nil,
			want: 0,
		},
	}
	for _, test := range tests {
		if got := exitCode(test.err); got != test.want {
			t.Errorf("exitCode(%#v) = %d; want %d", test.err, got, test.want)
		}
	}

	t.Run("ExitError", func(t *testing.T) {
		falsePath, err := exec.LookPath("false")
		if err != nil {
			t.Skip("could not find false:", err)
		}
		exitError := exec.Command(falsePath).Run()
		t.Logf("ran %s, got: %v", falsePath, exitError)
		if got, want := exitCode(exitError), 1; got != want {
			t.Errorf("exitCode(exitError) = %d; want %d", got, want)
		}
	})
}

type fakeExitError int

func (e fakeExitError) Error() string {
	if e == -1 {
		return "generic error"
	}
	return fmt.Sprintf("exit code %d", e.ExitCode())
}

func (e fakeExitError) ExitCode() int {
	return int(e)
}
