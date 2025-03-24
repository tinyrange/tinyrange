package libstruct

import (
	"fmt"
	"strconv"
)

var builtinSizes = map[TypeInstanceBuiltin]int64{
	TypeInstanceBuiltinChar:     1,
	TypeInstanceBuiltinUint8:    1,
	TypeInstanceBuiltinUint16LE: 2,
	TypeInstanceBuiltinUint32LE: 4,
	TypeInstanceBuiltinUint64LE: 8,
}

func EvaluateExpression(expr Expression) (int64, error) {
	switch expr := expr.(type) {
	case NumberLiteralExpression:
		val, err := strconv.ParseInt(string(expr), 0, 64)
		if err != nil {
			return 0, fmt.Errorf("failed to parse number literal: %w", err)
		}

		return val, nil
	case *BinaryExpression:
		lhs, err := EvaluateExpression(expr.Lhs)
		if err != nil {
			return 0, fmt.Errorf("failed to evaluate left expression: %w", err)
		}

		rhs, err := EvaluateExpression(expr.Rhs)
		if err != nil {
			return 0, fmt.Errorf("failed to evaluate right expression: %w", err)
		}

		switch expr.Op {
		case BinaryOpPlus:
			return lhs + rhs, nil
		case BinaryOpMinus:
			return lhs - rhs, nil
		default:
			return 0, fmt.Errorf("unexpected binary operator: %v", expr.Op)
		}
	default:
		return 0, fmt.Errorf("unexpected expression type: %T", expr)
	}
}

type ReferenceResolver func(TypeInstanceReference) (TypeInstance, error)

func SizeOf(typ TypeInstance, ref ReferenceResolver) (int64, error) {
	switch typ := typ.(type) {
	case *StructDeclaration:
		var size int64
		for _, member := range typ.Members {
			switch member := member.(type) {
			case *StructSizeMember:
				return EvaluateExpression(member.Size)
			default:
				return 0, fmt.Errorf("unexpected member type: %T", member)
			}
		}
		return size, nil
	case *UnionDeclaration:
		var size int64

		for _, member := range typ.Members {
			switch member := member.(type) {
			case *StructOrUnionMember:
				memberSize, err := SizeOf(member.ValueType, ref)
				if err != nil {
					return 0, fmt.Errorf("failed to get size of member: %w", err)
				}

				size = max(size, memberSize)
			default:
				return 0, fmt.Errorf("unexpected member type: %T", member)
			}
		}

		return size, nil
	case *EnumDeclaration:
		return SizeOf(typ.ValueType, ref)
	case *TypeInstanceArray:
		elementSize, err := SizeOf(typ.ElementType, ref)
		if err != nil {
			return 0, fmt.Errorf("failed to get size of element type: %w", err)
		}

		elementCount, err := EvaluateExpression(typ.Size)
		if err != nil {
			return 0, fmt.Errorf("failed to evaluate size expression: %w", err)
		}

		return elementSize * elementCount, nil
	case TypeInstanceBuiltin:
		val, ok := builtinSizes[typ]
		if !ok {
			return 0, fmt.Errorf("unknown builtin type: %s", typ)
		}

		return val, nil
	case TypeInstanceReference:
		inst, err := ref(typ)
		if err != nil {
			return 0, fmt.Errorf("failed to resolve reference: %w", err)
		}

		return SizeOf(inst, ref)
	default:
		return 0, fmt.Errorf("unexpected type: %T", typ)
	}
}
