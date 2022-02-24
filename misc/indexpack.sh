#!/bin/bash
# Copyright 2021 The gg Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail
if [[ $# -eq 0 ]]; then
  echo "usage: indexpack.sh PACKFILE [...]" 1>&2
  exit 64
fi
for packfile in "$@"; do
  idx1="${packfile/%.pack/.idx1}"
  git index-pack --index-version=1 -o "$idx1" "$packfile"
  idx2="${packfile/%.pack/.idx2}"
  git index-pack --index-version=2 -o "$idx2" "$packfile"
  # git-index-pack does not set the writable bit.
  chmod u+w "$idx1" "$idx2"
done
