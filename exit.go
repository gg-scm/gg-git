// Copyright 2018 The gg Authors
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

package git

import "errors"

// exitCode returns the exit code indicated by the error,
// zero if the error is nil,
// or -1 if the error doesn't indicate an exited process.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var coder interface {
		ExitCode() int
	}
	if !errors.As(err, &coder) {
		return -1
	}
	return coder.ExitCode()
}
