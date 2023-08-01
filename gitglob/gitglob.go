// Copyright 2023 The gg Authors
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

// Package gitglob provides a pure Go implementation of [Git glob] patterns.
//
// [Git glob]: https://git-scm.com/docs/gitglossary#Documentation/gitglossary.txt-glob
package gitglob

import (
	"fmt"
	"path"
	"strings"
	"unicode/utf8"
)

// Match reports whether name matches the glob pattern.
// If the pattern is malformed, then Match returns [path.ErrBadPattern].
func Match(pattern, name string) (matched bool, err error) {
	g, err := Compile(pattern)
	if err != nil {
		return false, path.ErrBadPattern
	}
	return g.MatchString(name), nil
}

type opcode uint8

const (
	// opChar checks whether the character at SP equals the rune x.
	// If not, abort the thread as failed.
	// Otherwise, increment SP and PC.
	opChar opcode = 1 + iota
	// opCharRange checks whether the character at SP
	// is greater than or equal to rune x
	// and less than or equal to rune y.
	// If not, abort the thread as failed.
	// Otherwise, increment SP and PC.
	opCharRange
	// opPeekNot checks whether the character at SP does not equal the rune x.
	// If it does, abort the thread as failed.
	// Otherwise, increment PC.
	// After a sequence of one or more opPeekNot and/or opPeekNotRange instructions,
	// there must be an opAny instruction.
	// Any other instruction is undefined behavior.
	opPeekNot
	// opPeekNotRange checks whether the character at SP
	// is less than rune x or greater than rune y.
	// If not, abort the thread as failed.
	// Otherwise, increment PC.
	// After a sequence of one or more opPeekNot and/or opPeekNotRange instructions,
	// there must be an opAny instruction.
	// Any other instruction is undefined behavior.
	opPeekNotRange
	// opAny increments SP and PC,
	// unless SP is at the end of the string, in which case it aborts the thread as failed.
	opAny
	// opJump sets PC to x.
	opJump
	// opSplit sets PC to x and creates a new thread with PC set to y.
	opSplit
	// opAbort aborts the thread as failed.
	opAbort
	// opEOF checks if SP is at the end of the string.
	// If not, abort the thread as failed.
	// Otherwise, increment PC.
	opEOF
)

type instruction struct {
	opcode opcode
	x      int
	y      int
}

// Glob is a compiled glob pattern.
type Glob struct {
	program []instruction
}

// Compile parses a glob pattern.
func Compile(pattern string) (*Glob, error) {
	g := &Glob{
		program: make([]instruction, 0, len(pattern)*4),
	}
	var ok bool
	if pattern, ok = cutPrefix(pattern, "**/"); ok {
		start := len(g.program)
		g.program = append(g.program,
			instruction{opcode: opSplit, x: start + 1, y: start + 5}, // 0
			instruction{opcode: opSplit, x: start + 2, y: start + 4}, // 1
			instruction{opcode: opAny},                               // 2
			instruction{opcode: opJump, x: start + 1},                // 3
			instruction{opcode: opChar, x: '/'},                      // 4
		)
	}

	for len(pattern) > 0 {
		if pattern == "/**" {
			// No need to anchor at EOF.
			g.program = append(g.program,
				instruction{opcode: opChar, x: '/'},
				instruction{opcode: opAny},
			)
			return g, nil
		}
		if pattern, ok = cutPrefix(pattern, "/**/"); ok {
			start := len(g.program)
			g.program = append(g.program,
				instruction{opcode: opChar, x: '/'},                      // 0
				instruction{opcode: opSplit, x: start + 2, y: start + 5}, // 1
				instruction{opcode: opAny},                               // 2
				instruction{opcode: opSplit, x: start + 2, y: start + 4}, // 3
				instruction{opcode: opChar, x: '/'},                      // 4
			)
			continue
		}
		switch pattern[0] {
		case '*':
			start := len(g.program)
			g.program = append(g.program,
				instruction{opcode: opSplit, x: start + 1, y: start + 4}, // 0
				instruction{opcode: opPeekNot, x: '/'},                   // 1
				instruction{opcode: opAny},                               // 2
				instruction{opcode: opJump, x: start},                    // 3
			)
			pattern = pattern[1:]
		case '?':
			g.program = append(g.program,
				instruction{opcode: opPeekNot, x: '/'},
				instruction{opcode: opAny},
			)
			pattern = pattern[1:]
		case '[':
			var err error
			pattern, err = compileCharClass(g, pattern)
			if err != nil {
				return nil, fmt.Errorf("gitglob: %v", err)
			}
		case '\\':
			if len(pattern) < 2 {
				return nil, fmt.Errorf("gitglob: cannot end in '\\'")
			}
			pattern = pattern[1:]
			fallthrough
		default:
			c, size := utf8.DecodeRuneInString(pattern)
			g.program = append(g.program, instruction{opcode: opChar, x: int(c)})
			pattern = pattern[size:]
		}
	}

	g.program = append(g.program, instruction{opcode: opEOF})
	return g, nil
}

func compileCharClass(g *Glob, pattern string) (string, error) {
	pattern, ok := cutPrefix(pattern, "[")
	if !ok {
		panic("called without starting on bracket")
	}
	pattern, invert := cutPrefix(pattern, "^")
	charClassStart := len(g.program)
	const endPlaceholder = -1
	for {
		if pattern == "" {
			return "", fmt.Errorf("unterminated character class")
		}
		switch pattern[0] {
		case ']':
			pattern = pattern[1:]
			if len(g.program) == charClassStart {
				return pattern, fmt.Errorf("empty character class")
			}

			if invert {
				// Inverted character classes just stack opPeekNot and opPeekNotRange.
				// Follow up with opAny and move on.
				g.program = append(g.program, instruction{opcode: opAny})
				return pattern, nil
			}

			// Fill in placeholders that jump to the end of the character class instructions.
			added := g.program[charClassStart:]
			for i := range added {
				switch added[i].opcode {
				case opJump:
					if added[i].x == endPlaceholder {
						added[i].x = len(g.program)
					}
				case opSplit:
					if added[i].x == endPlaceholder {
						added[i].x = len(g.program)
					}
					if added[i].y == endPlaceholder {
						added[i].y = len(g.program)
					}
				}
			}
			return pattern, nil
		case '-':
			return pattern, fmt.Errorf("invalid character class range")
		case '\\':
			if len(pattern) < 2 {
				return pattern, fmt.Errorf("unterminated character class")
			}
			pattern = pattern[1:]
			fallthrough
		default:
			c, size := utf8.DecodeRuneInString(pattern)
			if len(pattern) > size && pattern[size] == '-' {
				// Range.
				lo := c
				pattern = pattern[size+1:]
				var escaped bool
				pattern, escaped = cutPrefix(pattern, `\`)
				if len(pattern) == 0 {
					return "", fmt.Errorf("unterminated character class")
				}
				if !escaped && (pattern[0] == ']' || pattern[0] == '-') {
					return pattern, fmt.Errorf("invalid character class range")
				}
				hi, size := utf8.DecodeRuneInString(pattern)
				pattern = pattern[size:]
				if hi < lo {
					return pattern, fmt.Errorf("invalid character class range (%q > %q)", lo, hi)
				}

				if invert {
					g.program = append(g.program, instruction{
						opcode: opPeekNotRange,
						x:      int(lo),
						y:      int(hi),
					})
				} else {
					start := len(g.program)
					g.program = append(g.program,
						instruction{opcode: opSplit, x: start + 1, y: start + 3}, // 0
						instruction{opcode: opCharRange, x: int(lo), y: int(hi)}, // 1
						instruction{opcode: opJump, x: endPlaceholder},           // 2
					)
				}
				continue
			}

			if invert {
				g.program = append(g.program, instruction{opcode: opPeekNot, x: int(c)})
			} else {
				start := len(g.program)
				g.program = append(g.program,
					instruction{opcode: opSplit, x: start + 1, y: start + 3}, // 0
					instruction{opcode: opChar, x: int(c)},                   // 1
					instruction{opcode: opJump, x: endPlaceholder},           // 2
				)
			}
			pattern = pattern[size:]
		}
	}
}

// MatchString reports whether the name matches the glob.
func (g *Glob) MatchString(name string) bool {
	var currList, nextList threadList
	currList.grow(len(g.program))
	nextList.grow(len(g.program))

	currList.add(g.program, 0)
	for _, c := range name {
		for _, pc := range currList.queue {
		execute:
			if pc >= len(g.program) {
				// Reached end of program. Successful match!
				return true
			}

			switch inst := g.program[pc]; inst.opcode {
			case opChar:
				if c == rune(inst.x) {
					nextList.add(g.program, pc+1)
				}
			case opCharRange:
				if rune(inst.x) <= c && c <= rune(inst.y) {
					nextList.add(g.program, pc+1)
				}
			case opPeekNot:
				if c != rune(inst.x) {
					pc++
					goto execute
				}
			case opPeekNotRange:
				if !(rune(inst.x) <= c && c <= rune(inst.y)) {
					pc++
					goto execute
				}
			case opAny:
				nextList.add(g.program, pc+1)
			case opEOF, opAbort:
				// Abort thread.
			case opJump, opSplit:
				panic("control flow should be handled by threadList.add")
			default:
				panic("unknown instruction")
			}
		}

		currList, nextList = nextList, currList
		nextList.clear()
	}

	for len(currList.queue) > 0 {
		for _, pc := range currList.queue {
			if pc >= len(g.program) {
				// Reached end of program. Successful match!
				return true
			}

			switch inst := g.program[pc]; inst.opcode {
			case opEOF:
				nextList.add(g.program, pc+1)
			case opChar, opCharRange, opAny, opAbort, opPeekNot, opPeekNotRange:
				// Abort thread.
				// (Even for peeks, since the only valid instruction after is an opAny.)
			case opJump, opSplit:
				panic("control flow should be handled by threadList.add")
			default:
				panic("unknown instruction")
			}

			currList, nextList = nextList, currList
			nextList.clear()
		}
	}

	return false
}

type threadList struct {
	queue []int
	set   map[int]struct{}
}

func newThreadList(n int) *threadList {
	return &threadList{
		queue: make([]int, 0, n),
		set:   make(map[int]struct{}, n),
	}
}

func (tl *threadList) grow(n int) {
	if cap(tl.queue) < n {
		newQueue := make([]int, len(tl.queue), n)
		copy(newQueue, tl.queue)
		tl.queue = newQueue
	}
	if tl.set == nil {
		tl.set = make(map[int]struct{}, n)
	}
}

func (tl *threadList) add(prog []instruction, pc int) {
	var inst instruction
	if pc < len(prog) {
		inst = prog[pc]
	}

	switch inst.opcode {
	case opJump:
		tl.add(prog, inst.x)
	case opSplit:
		tl.add(prog, inst.x)
		tl.add(prog, inst.y)
	default:
		if _, present := tl.set[pc]; !present {
			tl.queue = append(tl.queue, pc)
			tl.set[pc] = struct{}{}
		}
	}
}

func (tl *threadList) clear() {
	tl.queue = tl.queue[:0]
	for k := range tl.set {
		delete(tl.set, k)
	}
}

func cutPrefix(s, prefix string) (after string, found bool) {
	if !strings.HasPrefix(s, prefix) {
		return s, false
	}
	return s[len(prefix):], true
}
