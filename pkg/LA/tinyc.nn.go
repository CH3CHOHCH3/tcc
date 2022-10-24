package LA

import (
	"bufio"
	"container/list"
	"fmt"
	"io"
	"os"
	"strings"
)

type frame struct {
	i            int
	s            string
	line, column int
}
type Lexer struct {
	// The lexer runs in its own goroutine, and communicates via channel 'ch'.
	ch      chan frame
	ch_stop chan bool
	// We record the level of nesting because the action could return, and a
	// subsequent call expects to pick up where it left off. In other words,
	// we're simulating a coroutine.
	// TODO: Support a channel-based variant that compatible with Go's yacc.
	stack []frame
	stale bool

	// The 'l' and 'c' fields were added for
	// https://github.com/wagerlabs/docker/blob/65694e801a7b80930961d70c69cba9f2465459be/buildfile.nex
	// Since then, I introduced the built-in Line() and Column() functions.
	l, c int

	parseResult interface{}

	// The following line makes it easy for scripts to insert fields in the
	// generated code.
	// [NEX_END_OF_LEXER_STRUCT]
}

// NewLexerWithInit creates a new Lexer object, runs the given callback on it,
// then returns it.
func NewLexerWithInit(in io.Reader, initFun func(*Lexer)) *Lexer {
	yylex := new(Lexer)
	if initFun != nil {
		initFun(yylex)
	}
	yylex.ch = make(chan frame)
	yylex.ch_stop = make(chan bool, 1)
	var scan func(in *bufio.Reader, ch chan frame, ch_stop chan bool, family []dfa, line, column int)
	scan = func(in *bufio.Reader, ch chan frame, ch_stop chan bool, family []dfa, line, column int) {
		// Index of DFA and length of highest-precedence match so far.
		matchi, matchn := 0, -1
		var buf []rune
		n := 0
		checkAccept := func(i int, st int) bool {
			// Higher precedence match? DFAs are run in parallel, so matchn is at most len(buf), hence we may omit the length equality check.
			if family[i].acc[st] && (matchn < n || matchi > i) {
				matchi, matchn = i, n
				return true
			}
			return false
		}
		var state [][2]int
		for i := 0; i < len(family); i++ {
			mark := make([]bool, len(family[i].startf))
			// Every DFA starts at state 0.
			st := 0
			for {
				state = append(state, [2]int{i, st})
				mark[st] = true
				// As we're at the start of input, follow all ^ transitions and append to our list of start states.
				st = family[i].startf[st]
				if -1 == st || mark[st] {
					break
				}
				// We only check for a match after at least one transition.
				checkAccept(i, st)
			}
		}
		atEOF := false
		stopped := false
		for {
			if n == len(buf) && !atEOF {
				r, _, err := in.ReadRune()
				switch err {
				case io.EOF:
					atEOF = true
				case nil:
					buf = append(buf, r)
				default:
					panic(err)
				}
			}
			if !atEOF {
				r := buf[n]
				n++
				var nextState [][2]int
				for _, x := range state {
					x[1] = family[x[0]].f[x[1]](r)
					if -1 == x[1] {
						continue
					}
					nextState = append(nextState, x)
					checkAccept(x[0], x[1])
				}
				state = nextState
			} else {
			dollar: // Handle $.
				for _, x := range state {
					mark := make([]bool, len(family[x[0]].endf))
					for {
						mark[x[1]] = true
						x[1] = family[x[0]].endf[x[1]]
						if -1 == x[1] || mark[x[1]] {
							break
						}
						if checkAccept(x[0], x[1]) {
							// Unlike before, we can break off the search. Now that we're at the end, there's no need to maintain the state of each DFA.
							break dollar
						}
					}
				}
				state = nil
			}

			if state == nil {
				lcUpdate := func(r rune) {
					if r == '\n' {
						line++
						column = 0
					} else {
						column++
					}
				}
				// All DFAs stuck. Return last match if it exists, otherwise advance by one rune and restart all DFAs.
				if matchn == -1 {
					if len(buf) == 0 { // This can only happen at the end of input.
						break
					}
					lcUpdate(buf[0])
					buf = buf[1:]
				} else {
					text := string(buf[:matchn])
					buf = buf[matchn:]
					matchn = -1
					select {
					case ch <- frame{matchi, text, line, column}:
						{
						}
					case stopped = <-ch_stop:
						{
						}
					}
					if stopped {
						break
					}
					if len(family[matchi].nest) > 0 {
						scan(bufio.NewReader(strings.NewReader(text)), ch, ch_stop, family[matchi].nest, line, column)
					}
					if atEOF {
						break
					}
					for _, r := range text {
						lcUpdate(r)
					}
				}
				n = 0
				for i := 0; i < len(family); i++ {
					state = append(state, [2]int{i, 0})
				}
			}
		}
		ch <- frame{-1, "", line, column}
	}
	go scan(bufio.NewReader(in), yylex.ch, yylex.ch_stop, dfas, 0, 0)
	return yylex
}

type dfa struct {
	acc          []bool           // Accepting states.
	f            []func(rune) int // Transitions.
	startf, endf []int            // Transitions at start and end of input.
	nest         []dfa
}

var dfas = []dfa{
	// [\n\r]+
	{[]bool{false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 10:
				return 1
			case 13:
				return 1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 10:
				return 1
			case 13:
				return 1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1}, []int{ /* End-of-input transitions */ -1, -1}, nil},

	// [ \t]+
	{[]bool{false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 9:
				return 1
			case 32:
				return 1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 9:
				return 1
			case 32:
				return 1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1}, []int{ /* End-of-input transitions */ -1, -1}, nil},

	// \+|-|\*|\/|%
	{[]bool{false, true, true, true, true, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 37:
				return 1
			case 42:
				return 2
			case 43:
				return 3
			case 45:
				return 4
			case 47:
				return 5
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 37:
				return -1
			case 42:
				return -1
			case 43:
				return -1
			case 45:
				return -1
			case 47:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 37:
				return -1
			case 42:
				return -1
			case 43:
				return -1
			case 45:
				return -1
			case 47:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 37:
				return -1
			case 42:
				return -1
			case 43:
				return -1
			case 45:
				return -1
			case 47:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 37:
				return -1
			case 42:
				return -1
			case 43:
				return -1
			case 45:
				return -1
			case 47:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 37:
				return -1
			case 42:
				return -1
			case 43:
				return -1
			case 45:
				return -1
			case 47:
				return -1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1, -1, -1, -1, -1}, []int{ /* End-of-input transitions */ -1, -1, -1, -1, -1, -1}, nil},

	// <=
	{[]bool{false, false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 60:
				return 1
			case 61:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 60:
				return -1
			case 61:
				return 2
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 60:
				return -1
			case 61:
				return -1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1, -1}, []int{ /* End-of-input transitions */ -1, -1, -1}, nil},

	// >=
	{[]bool{false, false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 61:
				return -1
			case 62:
				return 1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 61:
				return 2
			case 62:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 61:
				return -1
			case 62:
				return -1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1, -1}, []int{ /* End-of-input transitions */ -1, -1, -1}, nil},

	// ==
	{[]bool{false, false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 61:
				return 1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 61:
				return 2
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 61:
				return -1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1, -1}, []int{ /* End-of-input transitions */ -1, -1, -1}, nil},

	// !=
	{[]bool{false, false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 33:
				return 1
			case 61:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 33:
				return -1
			case 61:
				return 2
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 33:
				return -1
			case 61:
				return -1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1, -1}, []int{ /* End-of-input transitions */ -1, -1, -1}, nil},

	// &&
	{[]bool{false, false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 38:
				return 1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 38:
				return 2
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 38:
				return -1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1, -1}, []int{ /* End-of-input transitions */ -1, -1, -1}, nil},

	// \|\|
	{[]bool{false, false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 124:
				return 1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 124:
				return 2
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 124:
				return -1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1, -1}, []int{ /* End-of-input transitions */ -1, -1, -1}, nil},

	// =
	{[]bool{false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 61:
				return 1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 61:
				return -1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1}, []int{ /* End-of-input transitions */ -1, -1}, nil},

	// ,
	{[]bool{false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 44:
				return 1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 44:
				return -1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1}, []int{ /* End-of-input transitions */ -1, -1}, nil},

	// ;
	{[]bool{false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 59:
				return 1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 59:
				return -1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1}, []int{ /* End-of-input transitions */ -1, -1}, nil},

	// !
	{[]bool{false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 33:
				return 1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 33:
				return -1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1}, []int{ /* End-of-input transitions */ -1, -1}, nil},

	// <
	{[]bool{false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 60:
				return 1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 60:
				return -1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1}, []int{ /* End-of-input transitions */ -1, -1}, nil},

	// >
	{[]bool{false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 62:
				return 1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 62:
				return -1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1}, []int{ /* End-of-input transitions */ -1, -1}, nil},

	// \(
	{[]bool{false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 40:
				return 1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 40:
				return -1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1}, []int{ /* End-of-input transitions */ -1, -1}, nil},

	// \)
	{[]bool{false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 41:
				return 1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 41:
				return -1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1}, []int{ /* End-of-input transitions */ -1, -1}, nil},

	// \{
	{[]bool{false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 123:
				return 1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 123:
				return -1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1}, []int{ /* End-of-input transitions */ -1, -1}, nil},

	// \}
	{[]bool{false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 125:
				return 1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 125:
				return -1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1}, []int{ /* End-of-input transitions */ -1, -1}, nil},

	// void
	{[]bool{false, false, false, false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 100:
				return -1
			case 105:
				return -1
			case 111:
				return -1
			case 118:
				return 1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 100:
				return -1
			case 105:
				return -1
			case 111:
				return 2
			case 118:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 100:
				return -1
			case 105:
				return 3
			case 111:
				return -1
			case 118:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 100:
				return 4
			case 105:
				return -1
			case 111:
				return -1
			case 118:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 100:
				return -1
			case 105:
				return -1
			case 111:
				return -1
			case 118:
				return -1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1, -1, -1, -1}, []int{ /* End-of-input transitions */ -1, -1, -1, -1, -1}, nil},

	// int
	{[]bool{false, false, false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 105:
				return 1
			case 110:
				return -1
			case 116:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 105:
				return -1
			case 110:
				return 2
			case 116:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 105:
				return -1
			case 110:
				return -1
			case 116:
				return 3
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 105:
				return -1
			case 110:
				return -1
			case 116:
				return -1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1, -1, -1}, []int{ /* End-of-input transitions */ -1, -1, -1, -1}, nil},

	// while
	{[]bool{false, false, false, false, false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 101:
				return -1
			case 104:
				return -1
			case 105:
				return -1
			case 108:
				return -1
			case 119:
				return 1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 101:
				return -1
			case 104:
				return 2
			case 105:
				return -1
			case 108:
				return -1
			case 119:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 101:
				return -1
			case 104:
				return -1
			case 105:
				return 3
			case 108:
				return -1
			case 119:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 101:
				return -1
			case 104:
				return -1
			case 105:
				return -1
			case 108:
				return 4
			case 119:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 101:
				return 5
			case 104:
				return -1
			case 105:
				return -1
			case 108:
				return -1
			case 119:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 101:
				return -1
			case 104:
				return -1
			case 105:
				return -1
			case 108:
				return -1
			case 119:
				return -1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1, -1, -1, -1, -1}, []int{ /* End-of-input transitions */ -1, -1, -1, -1, -1, -1}, nil},

	// if
	{[]bool{false, false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 102:
				return -1
			case 105:
				return 1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 102:
				return 2
			case 105:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 102:
				return -1
			case 105:
				return -1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1, -1}, []int{ /* End-of-input transitions */ -1, -1, -1}, nil},

	// else
	{[]bool{false, false, false, false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 101:
				return 1
			case 108:
				return -1
			case 115:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 101:
				return -1
			case 108:
				return 2
			case 115:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 101:
				return -1
			case 108:
				return -1
			case 115:
				return 3
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 101:
				return 4
			case 108:
				return -1
			case 115:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 101:
				return -1
			case 108:
				return -1
			case 115:
				return -1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1, -1, -1, -1}, []int{ /* End-of-input transitions */ -1, -1, -1, -1, -1}, nil},

	// return
	{[]bool{false, false, false, false, false, false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 101:
				return -1
			case 110:
				return -1
			case 114:
				return 1
			case 116:
				return -1
			case 117:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 101:
				return 2
			case 110:
				return -1
			case 114:
				return -1
			case 116:
				return -1
			case 117:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 101:
				return -1
			case 110:
				return -1
			case 114:
				return -1
			case 116:
				return 3
			case 117:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 101:
				return -1
			case 110:
				return -1
			case 114:
				return -1
			case 116:
				return -1
			case 117:
				return 4
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 101:
				return -1
			case 110:
				return -1
			case 114:
				return 5
			case 116:
				return -1
			case 117:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 101:
				return -1
			case 110:
				return 6
			case 114:
				return -1
			case 116:
				return -1
			case 117:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 101:
				return -1
			case 110:
				return -1
			case 114:
				return -1
			case 116:
				return -1
			case 117:
				return -1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1, -1, -1, -1, -1, -1}, []int{ /* End-of-input transitions */ -1, -1, -1, -1, -1, -1, -1}, nil},

	// break
	{[]bool{false, false, false, false, false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 97:
				return -1
			case 98:
				return 1
			case 101:
				return -1
			case 107:
				return -1
			case 114:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 97:
				return -1
			case 98:
				return -1
			case 101:
				return -1
			case 107:
				return -1
			case 114:
				return 2
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 97:
				return -1
			case 98:
				return -1
			case 101:
				return 3
			case 107:
				return -1
			case 114:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 97:
				return 4
			case 98:
				return -1
			case 101:
				return -1
			case 107:
				return -1
			case 114:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 97:
				return -1
			case 98:
				return -1
			case 101:
				return -1
			case 107:
				return 5
			case 114:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 97:
				return -1
			case 98:
				return -1
			case 101:
				return -1
			case 107:
				return -1
			case 114:
				return -1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1, -1, -1, -1, -1}, []int{ /* End-of-input transitions */ -1, -1, -1, -1, -1, -1}, nil},

	// continue
	{[]bool{false, false, false, false, false, false, false, false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 99:
				return 1
			case 101:
				return -1
			case 105:
				return -1
			case 110:
				return -1
			case 111:
				return -1
			case 116:
				return -1
			case 117:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 99:
				return -1
			case 101:
				return -1
			case 105:
				return -1
			case 110:
				return -1
			case 111:
				return 2
			case 116:
				return -1
			case 117:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 99:
				return -1
			case 101:
				return -1
			case 105:
				return -1
			case 110:
				return 3
			case 111:
				return -1
			case 116:
				return -1
			case 117:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 99:
				return -1
			case 101:
				return -1
			case 105:
				return -1
			case 110:
				return -1
			case 111:
				return -1
			case 116:
				return 4
			case 117:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 99:
				return -1
			case 101:
				return -1
			case 105:
				return 5
			case 110:
				return -1
			case 111:
				return -1
			case 116:
				return -1
			case 117:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 99:
				return -1
			case 101:
				return -1
			case 105:
				return -1
			case 110:
				return 6
			case 111:
				return -1
			case 116:
				return -1
			case 117:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 99:
				return -1
			case 101:
				return -1
			case 105:
				return -1
			case 110:
				return -1
			case 111:
				return -1
			case 116:
				return -1
			case 117:
				return 7
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 99:
				return -1
			case 101:
				return 8
			case 105:
				return -1
			case 110:
				return -1
			case 111:
				return -1
			case 116:
				return -1
			case 117:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 99:
				return -1
			case 101:
				return -1
			case 105:
				return -1
			case 110:
				return -1
			case 111:
				return -1
			case 116:
				return -1
			case 117:
				return -1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1, -1, -1, -1, -1, -1, -1, -1}, []int{ /* End-of-input transitions */ -1, -1, -1, -1, -1, -1, -1, -1, -1}, nil},

	// [0-9]+
	{[]bool{false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch {
			case 48 <= r && r <= 57:
				return 1
			}
			return -1
		},
		func(r rune) int {
			switch {
			case 48 <= r && r <= 57:
				return 1
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1}, []int{ /* End-of-input transitions */ -1, -1}, nil},

	// [a-zA-Z_][a-zA-Z0-9]*
	{[]bool{false, true, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 95:
				return 1
			}
			switch {
			case 48 <= r && r <= 57:
				return -1
			case 65 <= r && r <= 90:
				return 1
			case 97 <= r && r <= 122:
				return 1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 95:
				return -1
			}
			switch {
			case 48 <= r && r <= 57:
				return 2
			case 65 <= r && r <= 90:
				return 2
			case 97 <= r && r <= 122:
				return 2
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 95:
				return -1
			}
			switch {
			case 48 <= r && r <= 57:
				return 2
			case 65 <= r && r <= 90:
				return 2
			case 97 <= r && r <= 122:
				return 2
			}
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1, -1}, []int{ /* End-of-input transitions */ -1, -1, -1}, nil},

	// "[^"]*"
	{[]bool{false, false, true, false}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 34:
				return 1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 34:
				return 2
			}
			return 3
		},
		func(r rune) int {
			switch r {
			case 34:
				return -1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 34:
				return 2
			}
			return 3
		},
	}, []int{ /* Start-of-input transitions */ -1, -1, -1, -1}, []int{ /* End-of-input transitions */ -1, -1, -1, -1}, nil},

	// "[^"\n\r]*
	{[]bool{false, true, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			switch r {
			case 10:
				return -1
			case 13:
				return -1
			case 34:
				return 1
			}
			return -1
		},
		func(r rune) int {
			switch r {
			case 10:
				return -1
			case 13:
				return -1
			case 34:
				return -1
			}
			return 2
		},
		func(r rune) int {
			switch r {
			case 10:
				return -1
			case 13:
				return -1
			case 34:
				return -1
			}
			return 2
		},
	}, []int{ /* Start-of-input transitions */ -1, -1, -1}, []int{ /* End-of-input transitions */ -1, -1, -1}, nil},

	// .
	{[]bool{false, true}, []func(rune) int{ // Transitions
		func(r rune) int {
			return 1
		},
		func(r rune) int {
			return -1
		},
	}, []int{ /* Start-of-input transitions */ -1, -1}, []int{ /* End-of-input transitions */ -1, -1}, nil},
}

func NewLexer(in io.Reader) *Lexer {
	return NewLexerWithInit(in, nil)
}

func (yyLex *Lexer) Stop() {
	yyLex.ch_stop <- true
}

// Text returns the matched text.
func (yylex *Lexer) Text() string {
	return yylex.stack[len(yylex.stack)-1].s
}

// Line returns the current line number.
// The first line is 0.
func (yylex *Lexer) Line() int {
	if len(yylex.stack) == 0 {
		return 0
	}
	return yylex.stack[len(yylex.stack)-1].line
}

// Column returns the current column number.
// The first column is 0.
func (yylex *Lexer) Column() int {
	if len(yylex.stack) == 0 {
		return 0
	}
	return yylex.stack[len(yylex.stack)-1].column
}

func (yylex *Lexer) next(lvl int) int {
	if lvl == len(yylex.stack) {
		l, c := 0, 0
		if lvl > 0 {
			l, c = yylex.stack[lvl-1].line, yylex.stack[lvl-1].column
		}
		yylex.stack = append(yylex.stack, frame{0, "", l, c})
	}
	if lvl == len(yylex.stack)-1 {
		p := &yylex.stack[lvl]
		*p = <-yylex.ch
		yylex.stale = false
	} else {
		yylex.stale = true
	}
	return yylex.stack[lvl].i
}
func (yylex *Lexer) pop() {
	yylex.stack = yylex.stack[:len(yylex.stack)-1]
}

type Token struct {
	class string
	text  string
}

func Parse(srcPath string) list.List {
	row := 1
	tokenList := list.New()
	src, err := os.Open(srcPath)
	if err != nil {
		fmt.Println(err)
		os.Exit(0)
	}
	defer src.Close()

	lex := NewLexer(src)
	txt := func() string {
		return lex.Text()
	}
	add := func(class string) {
		tokenList.PushBack(Token{class, txt()})
	}
	func(yylex *Lexer) {
	OUTER0:
		for {
			switch yylex.next(0) {
			case 0:
				{
					row++
				}
			case 1:
				{
				}
			case 2:
				{
					add("operator")
				}
			case 3:
				{
					add("<=")
				}
			case 4:
				{
					add(">=")
				}
			case 5:
				{
					add("==")
				}
			case 6:
				{
					add("!=")
				}
			case 7:
				{
					add("&&")
				}
			case 8:
				{
					add("||")
				}
			case 9:
				{
					add("=")
				}
			case 10:
				{
					add(",")
				}
			case 11:
				{
					add(";")
				}
			case 12:
				{
					add("!")
				}
			case 13:
				{
					add("<")
				}
			case 14:
				{
					add(">")
				}
			case 15:
				{
					add("(")
				}
			case 16:
				{
					add(")")
				}
			case 17:
				{
					add("lcb")
				}
			case 18:
				{
					add("rcb")
				}
			case 19:
				{
					add("void")
				}
			case 20:
				{
					add("int")
				}
			case 21:
				{
					add("while")
				}
			case 22:
				{
					add("if")
				}
			case 23:
				{
					add("else")
				}
			case 24:
				{
					add("return")
				}
			case 25:
				{
					add("break")
				}
			case 26:
				{
					add("continue")
				}
			case 27:
				{
					add("intcont")
				}
			case 28:
				{
					add("id")
				}
			case 29:
				{
					add("string")
				}
			case 30:
				{
					fmt.Printf("Error: Unterminated string constant at line %d: %s\n", row, txt())
					os.Exit(0)
				}
			case 31:
				{
					fmt.Printf("Error: Unrecognized character at line %d: %s.\n", row, txt())
					os.Exit(0)
				}
			default:
				break OUTER0
			}
			continue
		}
		yylex.pop()

	}(lex)
	return *tokenList
}
