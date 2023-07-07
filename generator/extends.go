package generator

import (
	"fmt"
	"go/types"
	"regexp"
	"strings"

	"github.com/dave/jennifer/jen"
	"github.com/pkg/errors"
	"github.com/rahimlis/go-converter/builder"
	"github.com/rahimlis/go-converter/xtype"
)

const (
	// packageNameSep separates between package path and name pattern
	// in goverter:extend input with package path.
	packageNameSep = ":"
)

// ParseExtendOptions holds extend method options.
type ParseExtendOptions struct {
	// PkgPath where the extend methods are located. If it is empty, the package is same as the
	// ConverterInterface package and ConverterScope should be used for the lookup.
	PkgPath string
	// Scope of the ConverterInterface.
	ConverterScope *types.Scope
	// ConverterInterface to use - can be nil if its use is not allowed.
	ConverterInterface types.Type
	// NamePattern is the regexp pattern to search for within the PkgPath above or
	// (if PkgPath is empty) within the Scope.
	NamePattern *regexp.Regexp
}

// parseExtendPackage parses the goverter:extend inputs with or without packages (local or external).
//
// extend statement can be one of the following:
// 1) local scope with a name: "ConvertAToB", it is also equivalent to ":ConvertAToB"
// 2) package with a name: "github.com/google/uuid:FromBytes"
// 3) either (1) or (2) with the above with a regexp pattern instead of a name
//
// To scan the whole package for candidate methods, use "package/path:.*".
// Note: if regexp pattern is used, only the methods matching the conversion signature can be used.
// Those are methods that have exactly one input (to convert from) and either one output (to covert to)
// or two outputs: type to convert to and an error object.
func (g *generator) parseExtendPackage(opts *ParseExtendOptions) error {
	if opts.PkgPath == "" {
		// search in the converter's scope
		loaded, err := g.searchExtendsInScope(opts.ConverterScope, opts)
		if err == nil && loaded == 0 {
			// no failure, but also nothing found (this can happen if pattern is used yet no matches found)
			err = fmt.Errorf("local package does not have methods with names that match "+
				"the golang regexp pattern %q and a convert signature", opts.NamePattern)
		}
		return err
	}

	return g.searchExtendsInPackages(opts)
}

// searchExtendsInPackages searches for extend conversion methods that match an input regexp pattern
// within a given package path.
// Note: if this method finds no candidates, it will report an error. Two reasons for that:
// scanning packages takes time and it is very likely a human error.
func (g *generator) searchExtendsInPackages(opts *ParseExtendOptions) error {
	// load a package by its path, loadPackages uses cache
	pkgs, err := g.loadPackages(opts.PkgPath)
	if err != nil {
		return err
	}

	var loaded int
	for _, pkg := range pkgs {
		// search in the scope of each package, first package is going to be the root one
		pkgLoaded, pkgErr := g.searchExtendsInScope(pkg.Types.Scope(), opts)
		if pkgErr != nil {
			if err == nil {
				// remember the first err only - it is likely the most relevant if name is exact
				err = pkgErr
			}
		} else {
			loaded += pkgLoaded
		}
	}

	if loaded == 0 {
		if err == nil {
			return fmt.Errorf(`package %s does not have methods with names that match
the golang regexp pattern %q and a convert signature`, opts.PkgPath, opts.NamePattern.String())
		}
		return errors.Wrap(err, "could not extend")
	}

	return nil
}

// searchExtendsInScope searches the given package scope (either local or external) for
// the conversion method candidates. See parseExtendPackage for more details.
// If the input scope is not local, always pass converterInterface as a nil.
func (g *generator) searchExtendsInScope(scope *types.Scope, opts *ParseExtendOptions) (int, error) {
	if prefix, complete := opts.NamePattern.LiteralPrefix(); complete {
		// this is not a regexp, use regular lookup and report error as is
		// we expect only one function to match
		return 1, g.parseExtendScopeMethod(scope, prefix, opts)
	}

	// this is regexp, scan thru the package methods to find funcs that match the pattern
	var loaded int
	for _, name := range scope.Names() {
		loc := opts.NamePattern.FindStringIndex(name)
		if len(loc) != 2 {
			continue
		}
		if loc[0] != 0 || loc[1] != len(name) {
			// we want full match only: e.g. CopyAbc.* won't match OtherCopyAbc
			continue
		}

		// must be a func
		obj := scope.Lookup(name)
		fn, ok := obj.(*types.Func)
		if !ok {
			// obj == nil also won't type cast
			continue
		}

		err := g.parseExtendFunc(fn, opts)
		if err == nil {
			loaded++
		}
	}
	return loaded, nil
}

func splitCustomMethod(fullMethod string) (path, name string, err error) {
	parts := strings.SplitN(fullMethod, packageNameSep, 2)
	switch len(parts) {
	case 0:
		return "", "", fmt.Errorf("Invalid custom method: %s", fullMethod)
	case 1:
		name = parts[0]
	case 2:
		path = parts[0]
		name = parts[1]
		if path == "" {
			// example: goverter:extend :MyLocalConvert
			// the purpose of the ':' in this case is confusing, do not allow such case
			return "", "", fmt.Errorf(`package path must not be empty in the custom method "%s".
See https://goverter.jmattheis.de/#/conversion/custom`, fullMethod)
		}
	}

	if name == "" {
		return "", "", fmt.Errorf(`method name pattern is required in the custom method "%s".
See https://goverter.jmattheis.de/#/conversion/custom`, fullMethod)
	}
	return
}

// parseExtend prepares a list of extend methods for use.
func (g *generator) parseExtend(converterInterface types.Type, converterScope *types.Scope, methods []string) error {
	for _, methodName := range methods {
		pkgPath, namePattern, err := splitCustomMethod(methodName)
		if err != nil {
			return err
		}

		pattern, err := regexp.Compile(namePattern)
		if err != nil {
			return errors.Wrapf(err, "could not parse name as regexp %q", namePattern)
		}

		opts := &ParseExtendOptions{
			ConverterScope:     converterScope,
			PkgPath:            pkgPath,
			NamePattern:        pattern,
			ConverterInterface: converterInterface,
		}

		err = g.parseExtendPackage(opts)
		if err != nil {
			return err
		}
	}
	return nil
}

// parseExtend prepares an extend conversion method using its name and a scope to search.
func (g *generator) parseExtendScopeMethod(scope *types.Scope, methodName string, opts *ParseExtendOptions) error {
	obj := scope.Lookup(methodName)
	if obj == nil {
		return fmt.Errorf("%s does not exist in scope", methodName)
	}

	fn, ok := obj.(*types.Func)
	if !ok {
		return fmt.Errorf("%s is not a function", methodName)
	}

	return g.parseExtendFunc(fn, opts)
}

// parseExtend prepares an extend conversion method using its func pointer.
func (g *generator) parseExtendFunc(fn *types.Func, opts *ParseExtendOptions) error {
	if !fn.Exported() {
		return fmt.Errorf("method %s is unexported", fn.Name())
	}

	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return fmt.Errorf("%s has no signature", fn.Name())
	}
	if sig.Params().Len() == 0 || sig.Results().Len() > 2 {
		return fmt.Errorf("%s has no or too many parameters", fn.Name())
	}
	if sig.Results().Len() == 0 || sig.Results().Len() > 2 {
		return fmt.Errorf("%s has no or too many returns", fn.Name())
	}

	source := sig.Params().At(0).Type()
	target := sig.Results().At(0).Type()
	returnError := false
	if sig.Results().Len() == 2 {
		if i, ok := sig.Results().At(1).Type().(*types.Named); ok && i.Obj().Name() == "error" && i.Obj().Pkg() == nil {
			returnError = true
		} else {
			return fmt.Errorf("second return parameter must have type error but had: %s", sig.Results().At(1).Type())
		}
	}

	selfAsFirstParameter := false
	if sig.Params().Len() == 2 {
		if opts.ConverterInterface == nil {
			// converterInterface is used when searching for methods in the local package only
			return fmt.Errorf("%s should have one parameter when using extend with a package", fn.Name())
		}
		if source.String() == opts.ConverterInterface.String() {
			selfAsFirstParameter = true
			source = sig.Params().At(1).Type()
		} else {
			return fmt.Errorf("the first parameter must be of type %s", opts.ConverterInterface.String())
		}
	}

	xsig := xtype.Signature{Source: source.String(), Target: target.String()}
	methodDef := &methodDefinition{
		ID:               fn.String(),
		Explicit:         true,
		Call:             jen.Qual(fn.Pkg().Path(), fn.Name()),
		Name:             fn.Name(),
		Source:           xtype.TypeOf(source),
		Target:           xtype.TypeOf(target),
		SelfAsFirstParam: selfAsFirstParameter,
		Flags:            g.flags,
		ReturnError:      returnError,
		ReturnTypeOrigin: fn.String(),
	}
	g.extend[xsig] = methodDef
	return nil
}

// parseExtend prepares an extend conversion method using its name and a scope to search.
func (g *generator) parseMapExtend(converter types.Type, scope *types.Scope, fullMethod string) (*builder.ExtendMethod, error) {
	pkgPath, name, err := splitCustomMethod(fullMethod)
	if err != nil {
		return nil, err
	}

	useScope := scope

	if pkgPath != "" {
		pkgs, err := g.loadPackages(pkgPath)
		if err != nil {
			return nil, err
		}
		if len(pkgs) != 1 {
			return nil, fmt.Errorf("'%s' package path matches multiple packages, it must match exactly one.", fullMethod)
		}
		useScope = pkgs[0].Types.Scope()
	}

	obj := useScope.Lookup(name)
	if obj == nil {
		return nil, fmt.Errorf("%s does not exist in scope", fullMethod)
	}

	fn, ok := obj.(*types.Func)
	if !ok {
		return nil, fmt.Errorf("%s is not a function", fullMethod)
	}

	if !fn.Exported() {
		return nil, fmt.Errorf("method %s is unexported", fullMethod)
	}

	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return nil, fmt.Errorf("%s has no signature", fullMethod)
	}
	if sig.Results().Len() == 0 || sig.Results().Len() > 2 {
		return nil, fmt.Errorf("%s has no or too many returns", fn.Name())
	}
	returnError := false
	if sig.Results().Len() == 2 {
		if i, ok := sig.Results().At(1).Type().(*types.Named); ok && i.Obj().Name() == "error" && i.Obj().Pkg() == nil {
			returnError = true
		} else {
			return nil, fmt.Errorf("second return parameter must have type error but had: %s", sig.Results().At(1).Type())
		}
	}

	methodDef := &builder.ExtendMethod{
		ID:          fn.String(),
		ReturnError: returnError,
		Call:        jen.Qual(fn.Pkg().Path(), fn.Name()),
		Name:        fn.Name(),
		Target:      xtype.TypeOf(sig.Results().At(0).Type()),
	}
	switch sig.Params().Len() {
	case 2:
		actual := sig.Params().At(0).Type().String()
		if actual != converter.String() {
			return nil, fmt.Errorf("%s has two parameters the first one must be the converter interface %s but was %s", fn.Name(), converter.String(), actual)
		}
		methodDef.SelfAsFirstParam = true
		methodDef.Source = xtype.TypeOf(sig.Params().At(1).Type())
	case 1:
		methodDef.Source = xtype.TypeOf(sig.Params().At(0).Type())
	case 0:
		// no parameter is fine
	default:
		return nil, fmt.Errorf("%s has too many parameters", fn.Name())
	}

	return methodDef, nil
}
