package main

import (
	"bufio"
	"bytes"
	"strconv"
	"encoding/json"
	"fmt"
	"log"
	"io"
	"os"
	"sort"
	"strings"
)

var (
	ignorePackage = map[string]bool{"runtime/cgo": true}
)

// Module representation for golang module
// https://golang.org/pkg/cmd/go/internal/list
type Module struct {
	Path     string
    Indirect bool         // is this module only an indirect dependency of main module?
    Dir      string       // directory holding files for this module, if any
}

// Package is the representation of package required
// for analysis
type Package struct {
	Dir            string
	ImportPath     string
	Standard       bool
	Ignore         bool
	Deps           []string
	Module         Module
	PackageInfo    PackageInfo
	ModuleInfo     ModuleInfo
}

// PackageInfo contains the information we need to
// take out after analysis for the package
type PackageInfo struct {
	IncomingDepdencyCount int
	OutgoingDependencyCount int
}

// ModuleInfo contains the information we need to
// take out after analysis for the module
type ModuleInfo struct {
	IncomingDepdencyCount int
	OutgoingDependencyCount int
}

// setIgnore sets the package needs to be ignored
func (p *Package) setIgnore() {
	if p.Standard == true {
		p.Ignore = true
		return
	}

	if ignorePackage[p.ImportPath] {
		p.Ignore = true
		return
	}

	if strings.HasPrefix(p.ImportPath, "k8s.io/") {
		p.Ignore = true
		return
	}

	p.Ignore = false
}

// setPacakgeInfo sets the package info with incoming and outgoing deps
func (p *Package) setPackageInfo(in int, out int) {
	p.PackageInfo = PackageInfo{
		IncomingDepdencyCount: in,
		OutgoingDependencyCount: out,
	}
}

// setModuleInfo sets the package info with incoming and outgoing deps
func (p *Package) setModuleInfo(in int, out int) {
	p.ModuleInfo = ModuleInfo{
		IncomingDepdencyCount: in,
		OutgoingDependencyCount: out,
	}
}

type Builder struct {
	packages []Package
	incomingPackages map[string]map[string]bool
	outgoingPackages map[string][]string
	ignoredPackages map[string]bool
	incomingModules map[string][]string
	outgoingModules map[string]map[string]bool
}

func NewBuilder() *Builder {
	return &Builder{
		packages: []Package{},
		incomingPackages: map[string]map[string]bool{},
		outgoingPackages: map[string][]string{},
		ignoredPackages: map[string]bool{},
		incomingModules: map[string][]string{},
		outgoingModules: map[string]map[string]bool{},
	}
}

// addPackages loads the packages reading the depedency file
func(b *Builder) addPackages(depFile *os.File) {
	decoder := json.NewDecoder(depFile)
	b.packages = []Package{}
	for {
		p := &Package{}
		err := decoder.Decode(p)
		if err == nil {
			b.packages = append(b.packages, *p)
			continue
		}
		if err == io.EOF {
			break
		}
		checkErr(err)
	}
}

func(b *Builder) setIgnoredPackages() {
	for i, _ := range b.packages {
		b.packages[i].setIgnore()
		if b.packages[i].Ignore == true {
			b.ignoredPackages[b.packages[i].ImportPath] = true
		}
	}
}

// evalDeps evaluates the incoming and outgoing dependencies
func(b *Builder) evalDeps() {
	// Evaluate depdencies package wise
	for _, pkg := range b.packages {
		if pkg.Ignore == true {
			continue
		}
		b.outgoingPackages[pkg.ImportPath] = pkg.Deps
		for _, dep := range pkg.Deps {
			if _, ok := b.ignoredPackages[dep]; ok {
				continue
			}
			if len(b.incomingPackages[dep]) == 0 {
				b.incomingPackages[dep] = map[string]bool{}
			}
			b.incomingPackages[dep][pkg.ImportPath] = true
		}
	}
	for i, _ := range b.packages {
		b.packages[i].setPackageInfo(
			len(b.incomingPackages[b.packages[i].ImportPath]),
			len(b.outgoingPackages[b.packages[i].ImportPath]),
		)
	}

	// Evaluate dependencies module wise
	for i, _ := range b.packages {
		fmt.Println(b.packages[i].ImportPath)
		if (b.packages[i].Module.Path == "") {
			fmt.Println(b.packages[i].Module.Path)
			for incomingPackage, _ := range b.incomingPackages[b.packages[i].ImportPath] {
				fmt.Println(incomingPackage)
				b.incomingModules[b.packages[i].Module.Path] = append(
					b.incomingModules[b.packages[i].Module.Path],
					incomingPackage,
				)
			}
			for _, outgoingPackage := range b.outgoingPackages[b.packages[i].ImportPath] {
				b.outgoingModules[b.packages[i].Module.Path][outgoingPackage] = true
			}
		}
	}
	for i, _ := range b.packages {
		if (b.packages[i].Module.Path != "") {
			b.packages[i].setModuleInfo(
				len(b.incomingModules[b.packages[i].Module.Path]),
				len(b.outgoingModules[b.packages[i].Module.Path]),
			)
		}
	}
}

// sortDeps sorts the depdendecies as Name then Incoming then Outgoing order
func(b *Builder) sortDeps() {
	sort.SliceStable(b.packages, func(i, j int) bool {
		return b.packages[i].ImportPath < b.packages[j].ImportPath
	})

	sort.SliceStable(b.packages, func(i, j int) bool {
		return b.packages[i].PackageInfo.IncomingDepdencyCount > b.packages[j].PackageInfo.IncomingDepdencyCount
	})

	sort.SliceStable(b.packages, func(i, j int) bool {
		return b.packages[i].PackageInfo.OutgoingDependencyCount > b.packages[j].PackageInfo.OutgoingDependencyCount
	})
}

func(b *Builder) showDepsPackageWise() {
	for _, pkg := range b.packages {
		if pkg.Ignore == true {
			continue
		}
		fmt.Printf("package: %s, incoming: %d, outgoing: %d\n", pkg.ImportPath, pkg.PackageInfo.IncomingDepdencyCount, pkg.PackageInfo.OutgoingDependencyCount)
	}
}

func(b *Builder) showDepsModuleWise() {
	for _, pkg := range b.packages {
		if pkg.Ignore == true {
			continue
		}
		if (pkg.Module.Path != "") {
			fmt.Printf("module: %s, incoming: %d, outgoing: %d\n", pkg.Module.Path, pkg.ModuleInfo.IncomingDepdencyCount, pkg.ModuleInfo.OutgoingDependencyCount)
		}
	}
}

// savePackageDeps save the result of analysis in the file
// this will help in seeing the diffs with every dependency change
func(b *Builder) savePackageDeps() {
	f, err := os.Create("./vendor/package-dependencies.csv")
	checkErr(err)
	defer f.Close()

	var buffer bytes.Buffer
	w := bufio.NewWriter(f)
	for _, pkg := range b.packages {
		if pkg.Ignore == true {
			continue
		}
		buffer.WriteString(pkg.ImportPath)
		buffer.WriteString(":")
		buffer.WriteString(strconv.Itoa(pkg.PackageInfo.IncomingDepdencyCount))
		buffer.WriteString(":")
		buffer.WriteString(strconv.Itoa(pkg.PackageInfo.OutgoingDependencyCount))
		buffer.WriteString("\n")
	}
	fmt.Fprint(
		w,
		buffer.String(),
		"\n",
	)
	err = w.Flush()
	checkErr(err)
}

// checkErr is the helper function to avoid writing err != nil
func checkErr(err error) {
	if err != nil {
		log.Fatalf("err: %v", err)
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: go run hack/eval-deps.go deps.json")
		fmt.Fprintln(os.Stderr, "deps.json is the output of `go list -mod=vendor -json -tags linux -e all`")
		os.Exit(1)
	}

	depFile, err := os.Open(os.Args[1])
	checkErr(err)
	defer depFile.Close()

	builder := NewBuilder()
	builder.addPackages(depFile)
	builder.setIgnoredPackages()
	builder.evalDeps()
	builder.sortDeps()
	// builder.showDepsPackageWise()
	builder.showDepsModuleWise()
	// builder.savePackageDeps()
	// builder.saveModuleDeps()
}
