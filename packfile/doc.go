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

/*
Package packfile provides types for operating on Git packfiles. Packfiles are
used for storing Git objects on disk and when sending Git objects over the
network. The format is described in https://git-scm.com/docs/pack-format.

Objects in a packfile may be either stored in their entirety or stored in a
"deltified" representation. Deltified objects are stored as a patch on top of
another object.
*/
package packfile
