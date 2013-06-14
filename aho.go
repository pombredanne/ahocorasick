// Copyright 2012 Google, Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package ahocorasick provides a simple implementation of Aho-Corasick
// string matching for go.
//
// This implementation allows you to very quickly match a number of
// strings against a stream of input, returning matched strings as they appear.
//
// Example:
//   aho := ahocorasick.NewAhoCorasick([]string{"abc", "abd", "abab", "blah"})
//   var input io.ByteReader = ...
//   for match := range aho.Match(input) {
//     fmt.Printf("Found match %q at index %d\n", match.Value, match.Index)
//   }
// There's also some helper functions so you don't have to convert to an
// io.ByteReader manually:
//   * MatchString - matches against a string
//   * MatchBytes  - matches against a []byte
//   * MatchReader - matches against an io.Reader
// Example:
//   aho := ahocorasick.NewAhoCorasick([]string{"abc", "abd", "abab", "blah"})
//   for match := range ahocorasick.MatchReader(os.Stdin, aho) {
//     fmt.Printf("Found match %q at index %d\n", match.Value, match.Index)
//   }
package ahocorasick

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
)

// acNode is a node within the Aho-Corasick parse tree.
type acNode struct {
	key          string
	suffix, dict *acNode
	transitions  transitionMap
	dictIndex    *int
}

// acTree stores the root node of the Aho-Corasick parse tree.
type acTree struct {
	root *acNode
}

// Match provides information on a single keyword within a byte-stream.
type Match struct {
	// The dictionary value found in the byte stream
	Value string
	// The start index of the value in the byte stream
	Index int
	// The index of this value in the dictionary used to create this AC matcher
	DictIndex int
}

// transition is used by transitionMap to store transitions within the AC tree.
type transition struct {
	b byte
	n *acNode
}

// transitionMap provides a sorted vector of transitions for nodes in the AC tree,
// with binary search to find the next node for a single byte.
// This seems to take about 50% more time than a [256]*acNode, but with a
// substantial size savings.
// Note:  This idea taken from http://cs.haifa.ac.il/~landau/gadi/shiri.pdf
type transitionMap []*transition

// Implements sort.Interface for our transitionMap
func (v transitionMap) Len() int {
	return len(v)
}
func (v transitionMap) Less(i, j int) bool {
	return v[i].b < v[j].b
}
func (v transitionMap) Swap(i, j int) {
	v[i], v[j] = v[j], v[i]
}

// add adds a transition to the transitionMap
func (v *transitionMap) add(b byte, n *acNode) {
	for _, e := range *v {
		if e.b == b {
			e.n = n
			return
		}
	}
	*v = append(*v, &transition{b, n})
}

// compile sorts our transition map so binary search will work
func (v transitionMap) compile() {
	sort.Sort(v)
}

// get returns the node we should transition to if we see the given byte,
// or nil if we have no viable transition.
func (v transitionMap) get(b byte) *acNode {
	top, bottom := len(v), 0
	for top > bottom {
		i := (top-bottom)/2 + bottom
		b2 := v[i].b
		if b2 > b {
			top = i
		} else if b2 < b {
			bottom = i + 1
		} else {
			return v[i].n
		}
	}
	return nil
}

// AhoCorasick provides Aho-Corsick matching for byte streams.
type AhoCorasick interface {
	// Match reads in bytes from its input and streams matches to the
	// channel it returns.  It closes the channel when it reaches the
	// EOF of the byte reader.
	//
	// Note: matches are returned in the order they're found, NOT the
	// order of their index.  So given the strings 'abcd' and 'b' in the
	// stream 'zabcdef', Match{'b', 2} will be returned BEFORE
	// Match{'abcd', 1}, since 'b' ends first.
	Match(io.ByteReader) chan Match
	// Matcher returns a Matcher object for doing your own byte-by-byte
	// matching, if you don't wanto to use a ByteReader/Channel.
	Matcher() Matcher
}

// MatchString is a helpful wrapper to look for matches in a string.
func MatchString(input string, aho AhoCorasick) chan Match {
	return MatchBytes([]byte(input), aho)
}

// MatchBytes is a helpful wrapper to look for matches in a []byte.
func MatchBytes(input []byte, aho AhoCorasick) chan Match {
	return aho.Match(bytes.NewBuffer(input))
}

// MatchReader is a helpful wrapper to look for matches in an io.Reader.
func MatchReader(input io.Reader, aho AhoCorasick) chan Match {
	return aho.Match(bufio.NewReader(input))
}

// NewAhoCorasick creates a new AhoCorasick string matcher that matches
// the given set of strings against byte streams.  Constructs the tree in O(n),
// where n is the total number of characters in all strings in the dictionary.
func NewAhoCorasick(dict []string) AhoCorasick {
	root := &acNode{
		key:         "",
		transitions: transitionMap{},
	}
	tree := &acTree{}
	tree.root = root
	substrs := map[string]*acNode{}
	substrs[""] = root
	// This first for loop constructs the Trie for all of our string elements
	// O(n) time, since the outer loop iterates over all strings, and the inner
	// loop iterates over each character, thus the combination loops once over
	// each character in all strings.
	for index, str := range dict {
		if str == "" {
			panic(errors.New("Can't look for empty string with Aho"))
		}
		from := root
		for j := 1; j <= len(str); j++ {
			substr := str[:j]
			to := substrs[substr]
			if to == nil {
				to = &acNode{
					key:         substr,
					transitions: transitionMap{},
				}
				substrs[substr] = to
			}
			if j == len(str) {
				di := index
				to.dictIndex = &di
			}
			b := str[j-1]
			from.transitions.add(b, to)
			from = to
		}
	}
	// This second for loop post-processes the Trie, making it Aho-Corasick
	// compatible.  It adds suffix and dictionary links to each internal node.
	// Again, this loop takes O(n) time, since the inner loops are done once per
	// character in a string, while the outer loop is done once per string.
	// There's a bunch of map lookups, but since they're hash maps, they're O(1)
	// apiece.
	for str, node := range substrs {
		// Compile all transitions, now that we've added everything.  This takes
		// O(c) time for each node, since the number of transitions is capped at
		// 256 (we're using bytes).
		node.transitions.compile()
		// All nodes WILL have a suffix link except root.  Nodes without other
		// suffix nodes will link to root.
		for i := 1; i <= len(str); i++ {
			suffix := str[i:]
			if sNode, ok := substrs[suffix]; ok {
				node.suffix = sNode
				break
			}
		}
		for i := 1; i < len(str); i++ {
			suffix := str[i:]
			if sNode, ok := substrs[suffix]; ok && sNode.dictIndex != nil {
				node.dict = sNode
				break
			}
		}
	}
	return tree
}

// Print prints out a node in an easy-to-read manner, for debugging.
func (n *acNode) Print() {
	fmt.Printf("Node %q dictIndex:%v\n", n.key, n.dictIndex)
	for _, t := range n.transitions {
		fmt.Printf("  transition: %q -> %q\n", t.b, t.n.key)
	}
	if n.suffix != nil {
		fmt.Printf("  suffix: %q\n", n.suffix.key)
	}
	if n.dict != nil {
		fmt.Printf("  dict: %q\n", n.dict.key)
	}
}

// Match implements the AhoCorasick.Match interface, matching input from
// an io.ByteReader against its internal tree and streaming matces out through
// its returned channel.  The internal tree is not modified during this,
// so code should be fully concurrency-safe.
func (t *acTree) Match(r io.ByteReader) chan Match {
	// We add a little extra space so we don't need to block immediately if we
	// find a string.  For inputs with lots of matches, this seems to have a good
	// affect on speed (~30-35% speedup).  In BenchmarkMatchingManyMatches
	// in aho_test.go, this brings processing time down from 485ns -> 320ns on
	// a 2.53GHz Intel Core i5.  Without matches, we regularly get 20-30ns per
	// character, so many matches still slows us down substantially.
	c := make(chan Match, 20)
	go t.match(r, c)
	return c
}

// Matcher allows you to do your own matching without a ByteReader, by passing
// in bytes one at a time via the Next call and seeing if there are as yet any
// matches.  Note that each Matcher assumes it's working from a single input
// stream, so Match.Index will be the number of bytes starting from the first
// byte the Matcher saw.
type Matcher interface {
	Next(b byte) []Match
}

type matcher struct {
	tree    *acTree
	current *acNode
	seek    int
}

func (t *acTree) Matcher() Matcher {
	return &matcher{tree: t}
}

// Next feeds the next byte in a stream to the matcher, and returns any Matches
// that have resulted because of the new byte... note that there may be 0, 1, or
// many.  For example, if you're searching for "abc" and "bc", the byte 'c'
// could return both of those if you've already input 'a' and 'b'.
func (m *matcher) Next(b byte) (out []Match) {
	m.seek++
	if m.current == nil {
		m.current = m.tree.root
	}
	for m.current != nil {
		if to := m.current.transitions.get(b); to != nil {
			m.current = to
			if m.current.dictIndex != nil {
				// We've hit a node whose substring is in our dictionary, so output its key
				out = append(out, Match{m.current.key, m.seek - len(m.current.key), *m.current.dictIndex})
			}
			// If this node links to others in the dictionary, output their keys
			for dict := m.current.dict; dict != nil; dict = dict.dict {
				out = append(out, Match{dict.key, m.seek - len(dict.key), *dict.dictIndex})
			}
			// We've found a transition in the graph, so we're done
			return
		}
		// No transition found yet, so check our suffix
		m.current = m.current.suffix
	}
	return nil
}

// match implements the internal logic for running a single match, reading
// in each byte of the ByteReader and traversing the internal AC tree to look
// for matches.
func (t *acTree) match(reader io.ByteReader, output chan Match) {
	m := t.Matcher()
	for b, err := reader.ReadByte(); err != io.EOF; b, err = reader.ReadByte() {
		for _, match := range m.Next(b) {
			output <- match
		}
	}
	close(output)
}
