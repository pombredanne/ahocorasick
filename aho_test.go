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

package ahocorasick

import (
	"fmt"
	"strings"
	"testing"
)

// checkResults checks the output of an AC match against a match slice.
func checkResults(match chan Match, expected []Match, t *testing.T) {
	i := 0
	for found := range match {
		if found != expected[i] {
			t.Error("Index", i, ": ", expected[i], "!=", found)
		}
		i++
	}
	if i != len(expected) {
		t.Error("Expected", len(expected), "outputs, got", i)
	}
}

// TestUsage tests basic usage of the matcher.
func TestUsage(t *testing.T) {
	ac := NewAhoCorasick([]string{
		"abcd",
		"caa",
		"fizzle",
		"zlef",
		"fizz",
		"c",
	})
	r := strings.NewReader("babcdaadabcaafizzlefizzle")
	expected := []Match{
		Match{"c", 3, 5},
		Match{"abcd", 1, 0},
		Match{"c", 10, 5},
		Match{"caa", 10, 1},
		Match{"fizz", 13, 4},
		Match{"fizzle", 13, 2},
		Match{"zlef", 16, 3},
		Match{"fizz", 19, 4},
		Match{"fizzle", 19, 2},
	}
	checkResults(ac.Match(r), expected, t)
}

func TestManyMatches(t *testing.T) {
	input := benchmarkValue(20)
	reader := strings.NewReader(input)
	ac := NewAhoCorasick([]string{
		"ababababababab",
		"ababab",
		"ababababab",
	})
	expected := []Match{
		Match{"ababab", 0, 1},
		Match{"ababab", 2, 1},
		Match{"ababababab", 0, 2},
		Match{"ababab", 4, 1},
		Match{"ababababab", 2, 2},
		Match{"ababab", 6, 1},
		Match{"ababababababab", 0, 0},
		Match{"ababababab", 4, 2},
		Match{"ababab", 8, 1},
		Match{"ababababababab", 2, 0},
		Match{"ababababab", 6, 2},
		Match{"ababab", 10, 1},
		Match{"ababababababab", 4, 0},
		Match{"ababababab", 8, 2},
		Match{"ababab", 12, 1},
		Match{"ababababababab", 6, 0},
		Match{"ababababab", 10, 2},
		Match{"ababab", 14, 1},
	}
	checkResults(ac.Match(reader), expected, t)
}

// TestTransitionMap tests that our transitionMap object correctly implements
// binary search.
func TestTransitionMap(t *testing.T) {
	nodes := []*acNode{}
	m := transitionMap{}
	expected := map[byte]*acNode{
		'a': nil,
		'z': nil,
	}
	for i := 0; i < 24; i++ {
		nodes = append(nodes, &acNode{key: string('b' + i)})
		m.add(byte('b'+i), nodes[i])
		expected[byte('b'+i)] = nodes[i]
	}
	m.compile()
	for b, n := range expected {
		actual := m.get(b)
		if actual != n {
			t.Error("Transition map failed for byte", string(b), "- expected", n, "got", actual)
		}
	}
}

// benchmarkValue returns a string of length n, of the form "abababab..."
func benchmarkValue(n int) string {
	input := []byte{}
	for i := 0; i < n; i++ {
		var b byte
		if i%2 == 0 {
			b = 'a'
		} else {
			b = 'b'
		}
		input = append(input, b)
	}
	return string(input)
}

// hardTree makes a particularly difficult to match tree for benchmarking.
// All strings of the form 'abab...abab[a-z]q' are matched.  This means that
// when checking against benchmarkValue strings, every node transition will
// have 26 possibilities (a-z) to choose from.  The 'q' at the end stops
// any actual matches from happening, since matches are expensive (adding a
// string to a channel, then blocking while it gets read off)
func hardTree() []string {
	ret := []string{}
	str := ""
	for i := 0; i < 2500; i++ {
		// We add a 'q' to the end to make sure we never actually match
		ret = append(ret, str+string('a'+(i%26))+"q")
		if i%26 == 25 {
			str = str + string('a'+len(str)%2)
		}
	}
	return ret
}

func BenchmarkMatchingNoMatch(b *testing.B) {
	b.StopTimer()
	reader := strings.NewReader(benchmarkValue(b.N))
	ac := NewAhoCorasick([]string{
		"abababababababd",
		"abababb",
		"abababababq",
	})
	b.StartTimer()
	for _ = range ac.Match(reader) {
	}
}

func BenchmarkMatchingManyMatches(b *testing.B) {
	b.StopTimer()
	reader := strings.NewReader(benchmarkValue(b.N))
	ac := NewAhoCorasick([]string{
		"ab",
		"ababababababab",
		"ababab",
		"ababababab",
	})
	b.StartTimer()
	for _ = range ac.Match(reader) {
	}
}

func BenchmarkMatchingHardTree(b *testing.B) {
	b.StopTimer()
	reader := strings.NewReader(benchmarkValue(b.N))
	ac := NewAhoCorasick(hardTree())
	b.StartTimer()
	for _ = range ac.Match(reader) {
	}
}

func ExampleAhoCorasick_Match() {
	dict := []string{"alpha", "habet", "sneeze", "hab", "bet", "mingle"}
	ac := NewAhoCorasick(dict)
	input := strings.NewReader("alphabetsoup")
	for m := range ac.Match(input) {
		fmt.Println(m.Value)
	}
	// Output:
	// alpha
	// hab
	// habet
	// bet
}
