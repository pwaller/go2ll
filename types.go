package main

import (
	"fmt"
	gotypes "go/types"
	"math/rand"

	irtypes "github.com/llir/llvm/ir/types"
)

func (t *translator) goToIRType(typ gotypes.Type) irtypes.Type {
	x, ok := t.goToIRTypeCache[typ]
	if ok {
		return x
	}

	var namedStruct *irtypes.StructType
	var namedSig *irtypes.StructType
	if isNamedStruct(typ) {
		// If the type is named, it might be recursive and need to refer to itself.
		namedStruct = &irtypes.StructType{}
		t.goToIRTypeCache[typ] = t.m.NewTypeDef(getTypeName(typ), namedStruct)
	} else if isNamedSignature(typ) {
		namedSig = &irtypes.StructType{}
		t.goToIRTypeCache[typ] = t.m.NewTypeDef(getTypeName(typ), namedSig)
	}

	x = t.goToIRTypeImpl(typ)

	if namedStruct != nil {
		namedStruct.Fields = x.(*irtypes.StructType).Fields
		x = namedStruct
	} else if namedSig != nil {
		*namedSig = *x.(*irtypes.StructType)
		x = namedSig
		x.SetName(getTypeName(typ))
	}

	t.goToIRTypeCache[typ] = x
	return x
}

func isNamedStruct(typ gotypes.Type) bool {
	named, ok := typ.(*gotypes.Named)
	if !ok {
		return false
	}
	_, ok = named.Underlying().(*gotypes.Struct)
	return ok
}

func isNamedSignature(typ gotypes.Type) bool {
	named, ok := typ.(*gotypes.Named)
	if !ok {
		return false
	}
	_, ok = named.Underlying().(*gotypes.Signature)
	return ok
}

// getTypeName makes a unique type for a name. Note that the 'qualified' type
// name might not be globally unique because named types may be defined within
// an inner scope (such as a function, or if block)
func getTypeName(typ gotypes.Type) string {
	// TODO(pwaller): Something better than a random int, which is a bit messy.
	return fmt.Sprintf("%s-%d", typ.String(), rand.Int())
}

func (t *translator) goToIRTypeImpl(typ gotypes.Type) irtypes.Type {
	typ = typ.Underlying()

	switch typ := typ.(type) {
	case *gotypes.Array:
		irElemType := t.goToIRType(typ.Elem())
		return irtypes.NewArray(uint64(typ.Len()), irElemType)

	case *gotypes.Basic:
		return t.goBasicToIRType(typ)

	case *gotypes.Chan:
		return irtypes.NewPointer(&irtypes.StructType{})

	case *gotypes.Interface:
		return irtypes.NewStruct(irtypes.I8Ptr, irtypes.I8Ptr)

	case *gotypes.Map:
		return irtypes.NewPointer(&irtypes.StructType{})

	// case *gotypes.Named:

	case *gotypes.Pointer:
		return irtypes.NewPointer(t.goToIRType(typ.Elem()))

	case *gotypes.Signature:
		var irRetType irtypes.Type = irtypes.Void
		goResults := typ.Results()
		switch {
		case goResults.Len() == 1:
			// Special case single-parameter to avoid the tuple in the result.
			irRetType = t.goToIRType(goResults.At(0).Type())
		case goResults.Len() > 1:
			irRetType = t.goToIRType(goResults)
		}

		var irParamTypes []irtypes.Type
		irParamTypes = append(irParamTypes, irtypes.I8Ptr)
		if typ.Recv() != nil {
			irParamTypes = append(irParamTypes, t.goToIRType(typ.Recv().Type()))
		}
		for i, n := 0, typ.Params().Len(); i < n; i++ {
			irParamTypes = append(irParamTypes, t.goToIRType(typ.Params().At(i).Type()))
		}

		irFunc := irtypes.NewFunc(irRetType, irParamTypes...)
		irFuncPtr := irtypes.NewPointer(irFunc)
		// { %funcType FuncPtr, i8* ClosureEnv }
		return irtypes.NewStruct(irFuncPtr, irtypes.I8Ptr)

	case *gotypes.Slice:
		irElemType := t.goToIRType(typ.Elem())
		return irtypes.NewStruct(
			irtypes.NewPointer(irElemType),
			irtypes.I64,
			irtypes.I64,
		)

	case *gotypes.Struct:
		var irFieldTypes []irtypes.Type
		for i, n := 0, typ.NumFields(); i < n; i++ {
			goFieldType := typ.Field(i).Type()
			irFieldType := t.goToIRType(goFieldType)
			irFieldTypes = append(irFieldTypes, irFieldType)
		}
		return irtypes.NewStruct(irFieldTypes...)

	case *gotypes.Tuple:
		var fields []irtypes.Type
		for i, n := 0, typ.Len(); i < n; i++ {
			fields = append(fields, t.goToIRType(typ.At(i).Type()))
		}
		return irtypes.NewStruct(fields...)

	default:
		panic(fmt.Sprintf("unimplemented type: %T: %s", typ, typ))
	}
}

var basicToIR = map[gotypes.BasicKind]irtypes.Type{
	gotypes.Invalid: irtypes.NewPointer(&irtypes.StructType{}),
	gotypes.Bool:    irtypes.I1,

	gotypes.Int:     irtypes.I64,
	gotypes.Int8:    irtypes.I8,
	gotypes.Int16:   irtypes.I16,
	gotypes.Int32:   irtypes.I32,
	gotypes.Int64:   irtypes.I64,
	gotypes.Uint:    irtypes.I64,
	gotypes.Uint8:   irtypes.I8,
	gotypes.Uint16:  irtypes.I16,
	gotypes.Uint32:  irtypes.I32,
	gotypes.Uint64:  irtypes.I64,
	gotypes.Uintptr: irtypes.I64,

	gotypes.Float32: irtypes.Float,
	gotypes.Float64: irtypes.Double,

	gotypes.Complex64:  irtypes.NewStruct(irtypes.Float, irtypes.Float),
	gotypes.Complex128: irtypes.NewStruct(irtypes.Double, irtypes.Double),

	gotypes.String: irtypes.NewStruct(irtypes.I8Ptr, irtypes.I64),

	gotypes.UnsafePointer: irtypes.I8Ptr,
}

func (t *translator) goBasicToIRType(typ *gotypes.Basic) irtypes.Type {
	irTyp, ok := basicToIR[typ.Kind()]
	if !ok {
		panic(fmt.Sprintf("unknown kind %v: %v", typ.Kind(), typ))
	}
	return irTyp
}

func isString(typ gotypes.Type) bool {
	basic, ok := typ.Underlying().(*gotypes.Basic)
	if !ok {
		return false
	}
	return basic.Kind() == gotypes.String || basic.Kind() == gotypes.UntypedString
}

func isFloat(typ gotypes.Type) bool {
	basic, ok := typ.Underlying().(*gotypes.Basic)
	if !ok {
		return false
	}

	return basic.Info()&gotypes.IsFloat != 0
}

func isComplex(typ gotypes.Type) bool {
	basic, ok := typ.Underlying().(*gotypes.Basic)
	if !ok {
		return false
	}

	return basic.Info()&gotypes.IsComplex != 0
}

func isInteger(typ gotypes.Type) bool {
	basic, ok := typ.Underlying().(*gotypes.Basic)
	if !ok {
		return false
	}

	return basic.Info()&gotypes.IsInteger != 0
}

func isSigned(typ gotypes.Type) bool {
	basic, ok := typ.Underlying().(*gotypes.Basic)
	if !ok {
		return false
	}

	return basic.Info()&gotypes.IsUnsigned == 0
}

func isBool(typ gotypes.Type) bool {
	basic, ok := typ.Underlying().(*gotypes.Basic)
	if !ok {
		return false
	}

	return basic.Info()&gotypes.IsBoolean != 0
}

func isPointer(typ gotypes.Type) bool {
	basic, ok := typ.Underlying().(*gotypes.Basic)
	if ok {
		return basic.Kind() == gotypes.UnsafePointer
	}
	_, ok = typ.Underlying().(*gotypes.Pointer)
	return ok
}

func isSlice(typ gotypes.Type) bool {
	_, ok := typ.Underlying().(*gotypes.Slice)
	return ok
}

func isInterface(typ gotypes.Type) bool {
	_, ok := typ.Underlying().(*gotypes.Interface)
	return ok
}

func isStruct(typ gotypes.Type) bool {
	_, ok := typ.Underlying().(*gotypes.Struct)
	return ok
}

func isArray(typ gotypes.Type) bool {
	_, ok := typ.Underlying().(*gotypes.Array)
	return ok
}

func isPtrToArray(typ gotypes.Type) bool {
	return isPointer(typ) && isArray(typ.(*gotypes.Pointer).Elem())
}

func isUnsafePointer(typ gotypes.Type) bool {
	basic, ok := typ.Underlying().(*gotypes.Basic)
	if !ok {
		return false
	}
	return basic.Kind() == gotypes.UnsafePointer
}

func isSignature(typ gotypes.Type) bool {
	_, ok := typ.Underlying().(*gotypes.Signature)
	return ok
}

func isChan(typ gotypes.Type) bool {
	_, ok := typ.Underlying().(*gotypes.Chan)
	return ok
}

func isMap(typ gotypes.Type) bool {
	_, ok := typ.Underlying().(*gotypes.Map)
	return ok
}

var sizeof = gotypes.SizesFor("gc", "amd64").Sizeof
