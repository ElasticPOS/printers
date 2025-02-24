// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build ignore
// +build ignore

/*
mksyscall_windows generates Windows system call bodies

It parses all files specified on command line containing function
prototypes (like syscall_windows.go) and prints system call bodies
to standard output.

The prototypes are marked by lines beginning with "//sys" and read
like func declarations if //sys is replaced by func, but:

  - The parameter lists must give a name for each argument. This
    includes return parameters.

  - The parameter lists must give a type for each argument:
    the (x, y, z int) shorthand is not allowed.

* If the return parameter is an error number, it must be named err.

  - If GO func name needs to be different from it's winapi dll name,
    the winapi name could be specified at the end, after "=" sign, like
    //sys LoadLibrary(libname string) (handle uint32, err error) = LoadLibraryA

  - Each function that returns err needs to supply a condition, that
    return value of winapi will be tested against to detect failure.
    This would set err to windows "last-error", otherwise it will be nil.
    The value can be provided at end of //sys declaration, like
    //sys LoadLibrary(libname string) (handle uint32, err error) [failretval==-1] = LoadLibraryA
    and is [failretval==0] by default.

Usage:

	mksyscall_windows [flags] [path ...]

The flags are:

	-output
		Specify output file name (outputs to console if blank).
	-trace
		Generate print statement after every syscall.
*/
package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"log"
	"os"
	"strings"
	"text/template"
)

var (
	filename       = flag.String("output", "", "output file name (standard output if omitted)")
	printTraceFlag = flag.Bool("trace", false, "generate print statement after every syscall")
)

func trim(s string) string {
	return strings.Trim(s, " \t")
}

var packageName string

func getPackageName() string {
	return packageName
}

func syscallDot() string {
	if packageName == "syscall" {
		return ""
	}
	return "syscall."
}

// Param is function parameter
type Param struct {
	Name      string
	Type      string
	fn        *Fn
	tmpVarIdx int
}

// tmpVar returns temp variable name that will be used to represent p during syscall.
func (p *Param) tmpVar() string {
	if p.tmpVarIdx < 0 {
		p.tmpVarIdx = p.fn.curTmpVarIdx
		p.fn.curTmpVarIdx++
	}
	return fmt.Sprintf("_p%d", p.tmpVarIdx)
}

// BoolTmpVarCode returns source code for bool temp variable.
func (p *Param) BoolTmpVarCode() string {
	const code = `var %s uint32
	if %s {
		%s = 1
	} else {
		%s = 0
	}`
	tmp := p.tmpVar()
	return fmt.Sprintf(code, tmp, p.Name, tmp, tmp)
}

// SliceTmpVarCode returns source code for slice temp variable.
func (p *Param) SliceTmpVarCode() string {
	const code = `var %s *%s
	if len(%s) > 0 {
		%s = &%s[0]
	}`
	tmp := p.tmpVar()
	return fmt.Sprintf(code, tmp, p.Type[2:], p.Name, tmp, p.Name)
}

// StringTmpVarCode returns source code for string temp variable.
func (p *Param) StringTmpVarCode() string {
	errStr := p.fn.Rets.ErrorVarName()
	if errStr == "" {
		errStr = "_"
	}
	tmp := p.tmpVar()
	const code = `var %s %s
	%s, %s = %s(%s)`
	s := fmt.Sprintf(code, tmp, p.fn.StrconvType(), tmp, errStr, p.fn.StrconvFunc(), p.Name)
	if errStr == "-" {
		return s
	}
	const moreCode = `
	if %s != nil {
		return
	}`
	return s + fmt.Sprintf(moreCode, errStr)
}

// TmpVarCode returns source code for temp variable.
func (p *Param) TmpVarCode() string {
	switch {
	case p.Type == "bool":
		return p.BoolTmpVarCode()
	case strings.HasPrefix(p.Type, "[]"):
		return p.SliceTmpVarCode()
	default:
		return ""
	}
}

// TmpVarHelperCode returns source code for helper's temp variable.
func (p *Param) TmpVarHelperCode() string {
	if p.Type != "string" {
		return ""
	}
	return p.StringTmpVarCode()
}

// SyscallArgList returns source code fragments representing p parameter
// in syscall. Slices are translated into 2 syscall parameters: pointer to
// the first element and length.
func (p *Param) SyscallArgList() []string {
	t := p.HelperType()
	var s string
	switch {
	case t[0] == '*':
		s = fmt.Sprintf("unsafe.Pointer(%s)", p.Name)
	case t == "bool":
		s = p.tmpVar()
	case strings.HasPrefix(t, "[]"):
		return []string{
			fmt.Sprintf("uintptr(unsafe.Pointer(%s))", p.tmpVar()),
			fmt.Sprintf("uintptr(len(%s))", p.Name),
		}
	default:
		s = p.Name
	}
	return []string{fmt.Sprintf("uintptr(%s)", s)}
}

// IsError determines if p parameter is used to return error.
func (p *Param) IsError() bool {
	return p.Name == "err" && p.Type == "error"
}

// HelperType returns type of parameter p used in helper function.
func (p *Param) HelperType() string {
	if p.Type == "string" {
		return p.fn.StrconvType()
	}
	return p.Type
}

// join concatenates parameters ps into a string with sep separator.
// Each parameter is converted into string by applying fn to it
// before conversion.
func join(ps []*Param, fn func(*Param) string, sep string) string {
	if len(ps) == 0 {
		return ""
	}
	a := make([]string, 0)
	for _, p := range ps {
		a = append(a, fn(p))
	}
	return strings.Join(a, sep)
}

// Rets describes function return parameters.
type Rets struct {
	Name         string
	Type         string
	ReturnsError bool
	FailCond     string
}

// ErrorVarName returns error variable name for r.
func (r *Rets) ErrorVarName() string {
	if r.ReturnsError {
		return "err"
	}
	if r.Type == "error" {
		return r.Name
	}
	return ""
}

// ToParams converts r into slice of *Param.
func (r *Rets) ToParams() []*Param {
	ps := make([]*Param, 0)
	if len(r.Name) > 0 {
		ps = append(ps, &Param{Name: r.Name, Type: r.Type})
	}
	if r.ReturnsError {
		ps = append(ps, &Param{Name: "err", Type: "error"})
	}
	return ps
}

// List returns source code of syscall return parameters.
func (r *Rets) List() string {
	s := join(r.ToParams(), func(p *Param) string { return p.Name + " " + p.Type }, ", ")
	if len(s) > 0 {
		s = "(" + s + ")"
	}
	return s
}

// PrintList returns source code of trace printing part correspondent
// to syscall return values.
func (r *Rets) PrintList() string {
	return join(r.ToParams(), func(p *Param) string { return fmt.Sprintf(`"%s=", %s, `, p.Name, p.Name) }, `", ", `)
}

// SetReturnValuesCode returns source code that accepts syscall return values.
func (r *Rets) SetReturnValuesCode() string {
	if r.Name == "" && !r.ReturnsError {
		return ""
	}
	retVal := "r0"
	if r.Name == "" {
		retVal = "r1"
	}
	errStr := "_"
	if r.ReturnsError {
		errStr = "e1"
	}
	return fmt.Sprintf("%s, _, %s := ", retVal, errStr)
}

func (r *Rets) useLongHandleErrorCode(inStr string) string {
	const code = `if %s {
		if e1 != 0 {
			err = error(e1)
		} else {
			err = %sEINVAL
		}
	}`
	cond := inStr + " == 0"
	if r.FailCond != "" {
		cond = strings.Replace(r.FailCond, "failretval", inStr, 1)
	}
	return fmt.Sprintf(code, cond, syscallDot())
}

// SetErrorCode returns source code that sets return parameters.
func (r *Rets) SetErrorCode() string {
	const code = `if r0 != 0 {
		%s = %sErrno(r0)
	}`
	if r.Name == "" && !r.ReturnsError {
		return ""
	}
	if r.Name == "" {
		return r.useLongHandleErrorCode("r1")
	}
	if r.Type == "error" {
		return fmt.Sprintf(code, r.Name, syscallDot())
	}
	s := ""
	switch {
	case r.Type[0] == '*':
		s = fmt.Sprintf("%s = (%s)(unsafe.Pointer(r0))", r.Name, r.Type)
	case r.Type == "bool":
		s = fmt.Sprintf("%s = r0 != 0", r.Name)
	default:
		s = fmt.Sprintf("%s = %s(r0)", r.Name, r.Type)
	}
	if !r.ReturnsError {
		return s
	}
	return s + "\n\t" + r.useLongHandleErrorCode(r.Name)
}

// Fn describes syscall function.
type Fn struct {
	Name        string
	Params      []*Param
	Rets        *Rets
	PrintTrace  bool
	dllName     string
	dllFuncName string
	src         string
	// TODO: get rid of this field and just use parameter index instead
	curTmpVarIdx int // insure tmp variables have uniq names
}

// extractParams parses s to extract function parameters.
func extractParams(s string, f *Fn) ([]*Param, error) {
	s = trim(s)
	if s == "" {
		return nil, nil
	}
	a := strings.Split(s, ",")
	ps := make([]*Param, len(a))
	for i := range ps {
		s2 := trim(a[i])
		b := strings.Split(s2, " ")
		if len(b) != 2 {
			b = strings.Split(s2, "\t")
			if len(b) != 2 {
				return nil, errors.New("Could not extract function parameter from \"" + s2 + "\"")
			}
		}
		ps[i] = &Param{
			Name:      trim(b[0]),
			Type:      trim(b[1]),
			fn:        f,
			tmpVarIdx: -1,
		}
	}
	return ps, nil
}

// extractSection extracts text out of string s starting after start
// and ending just before end. found return value will indicate success,
// and prefix, body and suffix will contain correspondent parts of string s.
func extractSection(s string, start, end rune) (prefix, body, suffix string, found bool) {
	s = trim(s)
	if strings.HasPrefix(s, string(start)) {
		// no prefix
		body = s[1:]
	} else {
		a := strings.SplitN(s, string(start), 2)
		if len(a) != 2 {
			return "", "", s, false
		}
		prefix = a[0]
		body = a[1]
	}
	a := strings.SplitN(body, string(end), 2)
	if len(a) != 2 {
		return "", "", "", false
	}
	return prefix, a[0], a[1], true
}

// newFn parses string s and return created function Fn.
func newFn(s string) (*Fn, error) {
	s = trim(s)
	f := &Fn{
		Rets:       &Rets{},
		src:        s,
		PrintTrace: *printTraceFlag,
	}
	// function name and args
	prefix, body, s, found := extractSection(s, '(', ')')
	if !found || prefix == "" {
		return nil, errors.New("Could not extract function name and parameters from \"" + f.src + "\"")
	}
	f.Name = prefix
	var err error
	f.Params, err = extractParams(body, f)
	if err != nil {
		return nil, err
	}
	// return values
	_, body, s, found = extractSection(s, '(', ')')
	if found {
		r, err := extractParams(body, f)
		if err != nil {
			return nil, err
		}
		switch len(r) {
		case 0:
		case 1:
			if r[0].IsError() {
				f.Rets.ReturnsError = true
			} else {
				f.Rets.Name = r[0].Name
				f.Rets.Type = r[0].Type
			}
		case 2:
			if !r[1].IsError() {
				return nil, errors.New("Only last windows error is allowed as second return value in \"" + f.src + "\"")
			}
			f.Rets.ReturnsError = true
			f.Rets.Name = r[0].Name
			f.Rets.Type = r[0].Type
		default:
			return nil, errors.New("Too many return values in \"" + f.src + "\"")
		}
	}
	// fail condition
	_, body, s, found = extractSection(s, '[', ']')
	if found {
		f.Rets.FailCond = body
	}
	// dll and dll function names
	s = trim(s)
	if s == "" {
		return f, nil
	}
	if !strings.HasPrefix(s, "=") {
		return nil, errors.New("Could not extract dll name from \"" + f.src + "\"")
	}
	s = trim(s[1:])
	a := strings.Split(s, ".")
	switch len(a) {
	case 1:
		f.dllFuncName = a[0]
	case 2:
		f.dllName = a[0]
		f.dllFuncName = a[1]
	default:
		return nil, errors.New("Could not extract dll name from \"" + f.src + "\"")
	}
	return f, nil
}

// DLLName returns DLL name for function f.
func (f *Fn) DLLName() string {
	if f.dllName == "" {
		return "kernel32"
	}
	return f.dllName
}

// DLLFuncName returns DLL function name for function f.
func (f *Fn) DLLFuncName() string {
	if f.dllFuncName == "" {
		return f.Name
	}
	return f.dllFuncName
}

// ParamList returns source code for function f parameters.
func (f *Fn) ParamList() string {
	return join(f.Params, func(p *Param) string { return p.Name + " " + p.Type }, ", ")
}

// HelperParamList returns source code for helper function f parameters.
func (f *Fn) HelperParamList() string {
	return join(f.Params, func(p *Param) string { return p.Name + " " + p.HelperType() }, ", ")
}

// ParamPrintList returns source code of trace printing part correspondent
// to syscall input parameters.
func (f *Fn) ParamPrintList() string {
	return join(f.Params, func(p *Param) string { return fmt.Sprintf(`"%s=", %s, `, p.Name, p.Name) }, `", ", `)
}

// ParamCount return number of syscall parameters for function f.
func (f *Fn) ParamCount() int {
	n := 0
	for _, p := range f.Params {
		n += len(p.SyscallArgList())
	}
	return n
}

// SyscallParamCount determines which version of Syscall/Syscall6/Syscall9/...
// to use. It returns parameter count for correspondent SyscallX function.
func (f *Fn) SyscallParamCount() int {
	n := f.ParamCount()
	switch {
	case n <= 3:
		return 3
	case n <= 6:
		return 6
	case n <= 9:
		return 9
	case n <= 12:
		return 12
	case n <= 15:
		return 15
	default:
		panic("too many arguments to system call")
	}
}

// Syscall determines which SyscallX function to use for function f.
func (f *Fn) Syscall() string {
	//c := f.SyscallParamCount()
	//if c == 3 {
	//	return syscallDot() + "Syscall"
	//}
	return syscallDot() + "SyscallN"
}

// SyscallParamList returns source code for SyscallX parameters for function f.
func (f *Fn) SyscallParamList() string {
	a := make([]string, 0)
	for _, p := range f.Params {
		a = append(a, p.SyscallArgList()...)
	}
	for len(a) < f.SyscallParamCount() {
		a = append(a, "0")
	}
	return strings.Join(a, ", ")
}

// HelperCallParamList returns source code of call into function f helper.
func (f *Fn) HelperCallParamList() string {
	a := make([]string, 0, len(f.Params))
	for _, p := range f.Params {
		s := p.Name
		if p.Type == "string" {
			s = p.tmpVar()
		}
		a = append(a, s)
	}
	return strings.Join(a, ", ")
}

// IsUTF16 is true, if f is W (utf16) function. It is false
// for all A (ascii) functions.
func (f *Fn) IsUTF16() bool {
	s := f.DLLFuncName()
	return s[len(s)-1] == 'W'
}

// StrconvFunc returns name of Go string to OS string function for f.
func (f *Fn) StrconvFunc() string {
	if f.IsUTF16() {
		return syscallDot() + "UTF16PtrFromString"
	}
	return syscallDot() + "BytePtrFromString"
}

// StrconvType returns Go type name used for OS string for f.
func (f *Fn) StrconvType() string {
	if f.IsUTF16() {
		return "*uint16"
	}
	return "*byte"
}

// HasStringParam is true, if f has at least one string parameter.
// Otherwise, it is false.
func (f *Fn) HasStringParam() bool {
	for _, p := range f.Params {
		if p.Type == "string" {
			return true
		}
	}
	return false
}

// HelperName returns name of function f helper.
func (f *Fn) HelperName() string {
	if !f.HasStringParam() {
		return f.Name
	}
	return "_" + f.Name
}

// Source files and functions.
type Source struct {
	Functions []*Fn
	Files     []string
}

// ParseFiles parses files listed in fs and extracts all syscall
// functions listed in  sys comments. It returns source files
// and functions collection *Source if successful.
func ParseFiles(fs []string) (*Source, error) {
	src := &Source{
		Functions: make([]*Fn, 0),
		Files:     make([]string, 0),
	}
	for _, file := range fs {
		if err := src.ParseFile(file); err != nil {
			return nil, err
		}
	}
	return src, nil
}

// DLLs return dll names for a source set src.
func (src *Source) DLLs() []string {
	uniq := make(map[string]bool)
	r := make([]string, 0)
	for _, f := range src.Functions {
		name := f.DLLName()
		if _, found := uniq[name]; !found {
			uniq[name] = true
			r = append(r, name)
		}
	}
	return r
}

// ParseFile adds addition file path to a source set src.
func (src *Source) ParseFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func(file *os.File) {
		_ = file.Close()
	}(file)

	s := bufio.NewScanner(file)
	for s.Scan() {
		t := trim(s.Text())
		if len(t) < 7 {
			continue
		}
		if !strings.HasPrefix(t, "//sys") {
			continue
		}
		t = t[5:]
		if !(t[0] == ' ' || t[0] == '\t') {
			continue
		}
		f, err := newFn(t[1:])
		if err != nil {
			return err
		}
		src.Functions = append(src.Functions, f)
	}
	if err := s.Err(); err != nil {
		return err
	}
	src.Files = append(src.Files, path)

	// get package name
	fileSet := token.NewFileSet()
	_, err = file.Seek(0, 0)
	if err != nil {
		return err
	}
	pkg, err := parser.ParseFile(fileSet, "", file, parser.PackageClauseOnly)
	if err != nil {
		return err
	}
	packageName = pkg.Name.Name

	return nil
}

// Generate output source file from a source set src.
func (src *Source) Generate(w io.Writer) error {
	funcMap := template.FuncMap{
		"packageName": getPackageName,
		"syscallDot":  syscallDot,
	}
	t := template.Must(template.New("main").Funcs(funcMap).Parse(srcTemplate))
	err := t.Execute(w, src)
	if err != nil {
		return errors.New("Failed to execute template: " + err.Error())
	}
	return nil
}

func usage() {
	_, _ = fmt.Fprintf(os.Stderr, "usage: mksyscall_windows [flags] [path ...]\n")
	flag.PrintDefaults()
	os.Exit(1)
}

func main() {
	flag.Usage = usage
	flag.Parse()
	if len(flag.Args()) <= 0 {
		_, _ = fmt.Fprintf(os.Stderr, "no files to parse provided\n")
		usage()
	}

	src, err := ParseFiles(flag.Args())
	if err != nil {
		log.Fatal(err)
	}

	var buf bytes.Buffer
	if err := src.Generate(&buf); err != nil {
		log.Fatal(err)
	}

	data, err := format.Source(buf.Bytes())
	if err != nil {
		log.Fatal(err)
	}
	if *filename == "" {
		_, err = os.Stdout.Write(data)
	} else {
		err = os.WriteFile(*filename, data, 0644)
	}
	if err != nil {
		log.Fatal(err)
	}
}

// TODO: use println instead to print in the following template
const srcTemplate = `

{{define "main"}}// MACHINE GENERATED BY 'go generate' COMMAND; DO NOT EDIT

package {{packageName}}

import (
	"unsafe"{{if syscallDot}}
	"syscall"{{end}}
)

var _ unsafe.Pointer

var (
{{template "dlls" .}}
{{template "funcNames" .}})
{{range .Functions}}{{if .HasStringParam}}{{template "helperBody" .}}{{end}}{{template "funcBody" .}}{{end}}
{{end}}

{{/* help functions */}}

{{define "dlls"}}{{range .DLLs}}	{{.}}Mod = {{syscallDot}}NewLazyDLL("{{.}}.drv")
{{end}}{{end}}

{{define "funcNames"}}{{range .Functions}}	proc{{.DLLFuncName}} = {{.DLLName}}Mod.NewProc("{{.DLLFuncName}}")
{{end}}{{end}}

{{define "helperBody"}}
func {{.Name}}({{.ParamList}}) {{template "results" .}}{
{{template "helperTmpVars" .}}	return {{.HelperName}}({{.HelperCallParamList}})
}
{{end}}

{{define "funcBody"}}
func {{.HelperName}}({{.HelperParamList}}) {{template "results" .}}{
{{template "tmpVars" .}}	{{template "syscall" .}}
{{template "setError" .}}{{template "printTrace" .}}	return
}
{{end}}

{{define "helperTmpVars"}}{{range .Params}}{{if .TmpVarHelperCode}}	{{.TmpVarHelperCode}}
{{end}}{{end}}{{end}}

{{define "tmpVars"}}{{range .Params}}{{if .TmpVarCode}}	{{.TmpVarCode}}
{{end}}{{end}}{{end}}

{{define "results"}}{{if .Rets.List}}{{.Rets.List}} {{end}}{{end}}

{{define "syscall"}}{{.Rets.SetReturnValuesCode}}{{.Syscall}}(proc{{.DLLFuncName}}.Addr(), {{.SyscallParamList}}){{end}}

{{define "setError"}}{{if .Rets.SetErrorCode}}	{{.Rets.SetErrorCode}}
{{end}}{{end}}

{{define "printTrace"}}{{if .PrintTrace}}	print("SYSCALL: {{.Name}}(", {{.ParamPrintList}}") (", {{.Rets.PrintList}}")\n")
{{end}}{{end}}

`
