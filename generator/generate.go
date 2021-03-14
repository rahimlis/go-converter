package generator

import (
	"fmt"
	"go/importer"
	"go/token"
	"go/types"

	"github.com/dave/jennifer/jen"
	"github.com/jmattheis/goverter/builder"
	"github.com/jmattheis/goverter/comments"
	"github.com/jmattheis/goverter/namer"
	"github.com/jmattheis/goverter/xtype"
)

type Config struct {
	Name string
}

var BuildSteps = []builder.Builder{
	&builder.BasicTargetPointerRule{},
	&builder.Pointer{},
	&builder.TargetPointer{},
	&builder.Basic{},
	&builder.Struct{},
	&builder.List{},
	&builder.Map{},
}

func Generate(pattern string, mapping []comments.Converter, config Config) (*jen.File, error) {
	sources, err := importer.ForCompiler(token.NewFileSet(), "source", nil).Import(pattern)
	if err != nil {
		return nil, err
	}
	file := jen.NewFile(config.Name)
	file.HeaderComment("// Code generated by github.com/jmattheis/goverter, DO NOT EDIT.")

	for _, converter := range mapping {
		obj := sources.Scope().Lookup(converter.Name)
		if obj == nil {
			return nil, fmt.Errorf("%s: could not find %s", pattern, converter.Name)
		}

		// create the converter struct
		file.Type().Id(converter.Config.Name).Struct()

		gen := Generator{
			namer:  namer.New(),
			file:   file,
			name:   converter.Config.Name,
			lookup: map[xtype.Signature]*Method{},
			extend: map[xtype.Signature]*Method{},
		}
		interf := obj.Type().Underlying().(*types.Interface)

		if err := gen.parseExtend(obj.Type(), sources.Scope(), converter.Config.ExtendMethods); err != nil {
			return nil, fmt.Errorf("Error while parsing extend methods: %s", err)
		}

		// we checked in comments, that it is an interface
		for i := 0; i < interf.NumMethods(); i++ {
			method := interf.Method(i)
			converterMethod := comments.Method{}

			if m, ok := converter.Methods[method.Name()]; ok {
				converterMethod = m
			}
			if err := gen.registerMethod(method, converterMethod); err != nil {
				return nil, fmt.Errorf("Error while creating converter method:\n    %s\n\n%s", method.String(), err)
			}
		}
		if err := gen.createMethods(); err != nil {
			return nil, err
		}
	}
	return file, nil
}
