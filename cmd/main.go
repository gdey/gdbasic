package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

type Value struct {
	Int   int
	Str   string
	IsStr bool
}

func (v Value) String() string {
	if v.IsStr {
		return fmt.Sprintf(`"%s"`, v.Str)
	}
	return fmt.Sprintf("%d", v.Int)
}
func (v Value) IntrepString(*Interpreter) (string, error) {
	if v.IsStr {
		return fmt.Sprintf("%s", v.Str), nil
	}
	return fmt.Sprintf("%d", v.Int), nil
}

type Reference string

func (ref Reference) IntrepString(intp *Interpreter) (string, error) {
	val, ok := intp.Variables[string(ref)]
	if !ok {
		return "", fmt.Errorf("unknown var: %v", string(ref))
	}
	return val.IntrepString(intp)
}

func (ref Reference) String() string { return string(ref) }

type IntrepreterStringer interface {
	fmt.Stringer
	IntrepString(*Interpreter) (string, error)
}

type Instructioner interface {
	fmt.Stringer
	Execute(*Interpreter) error
}

func IsString(s string) bool {
	str := strings.TrimSpace(s)
	if len(str) == 0 {
		return true
	}
	return str[0] == '"' && len(str) >= 2 && str[len(str)-1] == '"'
}
func getString(s string) string {
	str := strings.TrimSpace(s)
	if len(str) <= 2 {
		return ""
	}
	return str[1 : len(str)-1]
}

func strValue(s string) Value {
	return Value{
		Str:   s,
		IsStr: true,
	}
}

func intStrValue(s string) (Value, error) {
	i64, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return Value{}, err
	}
	return Value{
		Int: int(i64),
	}, nil
}

type PrintInstruction struct {
	strings   []IntrepreterStringer
	NoNewline bool
}

func (pi PrintInstruction) Execute(inter *Interpreter) error {
	for _, val := range pi.strings {
		s, err := val.IntrepString(inter)
		if err != nil {
			return err
		}
		fmt.Printf("%s", s)
	}
	if !pi.NoNewline {
		fmt.Println()
	}
	return nil
}
func (pi PrintInstruction) String() string {

	var buf strings.Builder
	semicolon := ""
	if pi.NoNewline {
		semicolon = ";"
	}

	for i := range pi.strings {
		strv := pi.strings[i].String()
		if i == 0 && strv[0] != '"' {
			buf.WriteRune(' ')
		} else if i != 0 {
			buf.WriteRune(';')
		}
		buf.WriteString(pi.strings[i].String())
	}
	return fmt.Sprintf("PRINT%s%s", buf.String(), semicolon)
}

func NewPrintInstruction(line int, remainder string) (pi *PrintInstruction, err error) {
	remainder = strings.TrimSpace(remainder)
	if len(remainder) == 0 {
		// just a newline
		return nil, nil
	}
	pi = new(PrintInstruction)
	pi.NoNewline = remainder[len(remainder)-1] == ';'

	var output strings.Builder

	parameters := strings.Split(remainder, ";")
	for i := range parameters {
		parameters[i] = strings.TrimSpace(parameters[i])
		if len(parameters[i]) == 0 {
			continue
		}
		switch {
		case IsString(parameters[i]):
			output.WriteString(getString(parameters[i]))
		case strings.IndexRune(parameters[i], '(') != -1:
			// functions
			switch {
			case strings.HasPrefix(parameters[i], "TAB("):
				// We have a tab.
				idx := strings.Index(parameters[i], ")")
				if idx == -1 || idx == 4 {
					return nil, fmt.Errorf("incomplete tab command")
				}
				num, err := strconv.Atoi(parameters[i][4:idx])
				if err != nil {
					return nil, fmt.Errorf("incomplete tab command")
				}
				output.WriteString(strings.Repeat(" ", num))

			default:
				return nil, fmt.Errorf("print: don't know how to handled func `%s`", parameters[i])
			}
		default:
			// assume a variable reference
			if output.Len() != 0 {
				pi.strings = append(pi.strings, strValue(output.String()))
				output.Reset()
			}
			pi.strings = append(pi.strings, Reference(parameters[i]))

		}
	}

	if output.Len() != 0 {
		pi.strings = append(pi.strings, strValue(output.String()))
	}

	return pi, err
}

type LetInstruction struct {
	VarName string
	Value   Value
}

func (li LetInstruction) Execute(intp *Interpreter) error {
	intp.Variables[li.VarName] = li.Value
	return nil
}

func (li LetInstruction) String() string {
	return fmt.Sprintf("LET %s=%s", li.VarName, li.Value)
}

func NewLetInstruction(_ int, remainder string) (*LetInstruction, error) {
	// LET A=1000
	idx := strings.Index(remainder, "=")
	if idx == -1 {
		return nil, fmt.Errorf("invalid let statment")
	}
	varName := remainder[:idx]
	varValue := remainder[idx+1:]
	if IsString(varValue) {
		return &LetInstruction{
			VarName: varName,
			Value:   strValue(getString(varValue)),
		}, nil
	}
	intValue, err := intStrValue(varValue)
	if err != nil {
		return nil, err
	}
	return &LetInstruction{
		VarName: varName,
		Value:   intValue,
	}, nil
}

type JumpInstruction int

func (jmp JumpInstruction) Execute(intp *Interpreter) error {
	return intp.SetPC(int(jmp))
}

func (jmp JumpInstruction) String() string {
	return fmt.Sprintf("GOTO %v", int(jmp))
}

func NewJumpInstruction(_ int, remainder string) (JumpInstruction, error) {

	i64, err := strconv.ParseInt(remainder, 10, 32)
	if err != nil {
		return JumpInstruction(0), fmt.Errorf("goto has a bad line number `%s`: %v", remainder, err)
	}
	return JumpInstruction(i64), nil
}

type Interpreter struct {
	Variables    map[string]Value
	Instructions map[int]Instructioner

	intructionIndex []int
	pc              int
}

func getCommandIdx(s string) (string, int) {
	idx := strings.IndexAny(s, ` "`)
	if idx == -1 {
		return s, idx
	}
	return s[:idx], idx
}

func (bob *Interpreter) Interpret(line string) error {
	if len(line) == 0 {
		return nil
	}
	// remove the line number
	idx := strings.Index(line, " ")
	if idx == -1 {
		return fmt.Errorf("DID NOT FIND A LINE NUMBER")
	}
	lineString := line[:idx]
	i64, err := strconv.ParseInt(lineString, 10, 32)
	if err != nil {
		return fmt.Errorf("bad line number `%s`: %v", lineString, err)

	}
	lineNumber := int(i64)
	line = line[idx+1:]

	var instruction Instructioner
	cmd, cmdIdx := getCommandIdx(line)
	remainder := ""
	if cmdIdx != -1 {
		remainder = strings.TrimSpace(line[cmdIdx:])
	}
	if cmd == "PRINT" {
		instruction, err = NewPrintInstruction(lineNumber, remainder)
		if err != nil {
			return err
		}
	}
	if cmd == "LET" {
		instruction, err = NewLetInstruction(lineNumber, remainder)
		if err != nil {
			return err
		}
	}
	if cmd == "GOTO" {
		instruction, err = NewJumpInstruction(lineNumber, remainder)
		if err != nil {
			return err
		}
	}

	if instruction == nil {
		return fmt.Errorf("unknown instruction: `%s` `%s`", cmd, remainder)
	}

	bob.Instructions[lineNumber] = instruction
	return nil
}
func (bob *Interpreter) buildInstructionIndex() error {
	if bob.intructionIndex != nil {
		return nil
	}
	bob.intructionIndex = make([]int, 0, len(bob.Instructions))
	for ln := range bob.Instructions {
		bob.intructionIndex = append(bob.intructionIndex, ln)
	}
	sort.Ints(bob.intructionIndex)
	for i := 1; i < len(bob.intructionIndex); i++ {
		if bob.intructionIndex[i-1] == bob.intructionIndex[i] {
			return fmt.Errorf("duplicate linenumber %v found", bob.intructionIndex[i])
		}
	}
	bob.pc = 0
	return nil
}
func (bob *Interpreter) SetPC(linenumber int) error {
	bob.buildInstructionIndex()
	idx := sort.SearchInts(bob.intructionIndex, linenumber)
	if len(bob.intructionIndex) == idx {
		return fmt.Errorf("did not find line number: %v", linenumber)
	}
	bob.pc = idx
	return nil
}

func (bob *Interpreter) Run() error {
	bob.buildInstructionIndex()
	var err error

	for bob.pc < len(bob.intructionIndex) {
		ln := bob.intructionIndex[bob.pc]
		bob.pc++
		instruction := bob.Instructions[ln]
		if err = instruction.Execute(bob); err != nil {
			return err
		}
	}
	return nil
}

func (bob *Interpreter) DumpMemory() {
	fmt.Printf("Instructions:\n")
	bob.buildInstructionIndex()
	zeroFill := 0 - (int(math.Log10(float64(bob.intructionIndex[len(bob.intructionIndex)-1]))) + 1)
	for _, key := range bob.intructionIndex {
		ins := bob.Instructions[key]
		if ins == nil {
			fmt.Printf("%*d nil instruction %#v \n", zeroFill, key, ins)
		}
		fmt.Printf("%*d %s\n", zeroFill, key, ins)
	}

	maxNameLen := 0
	names := make([]string, 0, len(bob.Variables))

	for name := range bob.Variables {
		if maxNameLen < len(name) {
			maxNameLen = len(name)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Printf("Variables:\n")
	for _, name := range names {
		fmt.Printf("% *s : %s\n", maxNameLen, name, bob.Variables[name].String())
	}

	fmt.Printf("done\n")
}

func NewInterpreter() *Interpreter {
	return &Interpreter{
		Instructions: map[int]Instructioner{},
		Variables:    map[string]Value{},
	}
}

func main() {

	flag.Parse()
	if flag.NArg() < 1 {
		fmt.Printf("need the basic file to interpret")
		os.Exit(1)
	}

	basicFilename := flag.Arg(0)

	file, err := os.Open(basicFilename)
	if err != nil {
		log.Fatalf("failed to open %s : %v", basicFilename, err)
	}
	defer file.Close()

	bob := NewInterpreter()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if err := bob.Interpret(scanner.Text()); err != nil {
			log.Fatal(err)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}
	bob.DumpMemory()
	if err = bob.Run(); err != nil {
		log.Fatal(err)
	}

}
