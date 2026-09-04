package staticgen

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

func anyLiteral(value any) (string, error) {
	if value == nil {
		return "nil", nil
	}
	return reflectedLiteral(reflect.ValueOf(value))
}

func reflectedLiteral(value reflect.Value) (string, error) {
	if !value.IsValid() {
		return "nil", nil
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return "nil", nil
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.String:
		return strconv.Quote(value.String()), nil
	case reflect.Bool:
		return strconv.FormatBool(value.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Kind().String() + "(" + strconv.FormatInt(value.Int(), 10) + ")", nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Kind().String() + "(" + strconv.FormatUint(value.Uint(), 10) + ")", nil
	case reflect.Float32, reflect.Float64:
		number := value.Float()
		if math.IsInf(number, 0) || math.IsNaN(number) {
			return "", fmt.Errorf("non-finite floating-point values are not supported")
		}
		return value.Kind().String() + "(" + strconv.FormatFloat(number, 'g', -1, value.Type().Bits()) + ")", nil
	case reflect.Slice, reflect.Array:
		var result strings.Builder
		result.WriteString("[]any{")
		for index := 0; index < value.Len(); index++ {
			literal, err := reflectedLiteral(value.Index(index))
			if err != nil {
				return "", err
			}
			result.WriteString(literal)
			result.WriteByte(',')
		}
		result.WriteByte('}')
		return result.String(), nil
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String {
			return "", fmt.Errorf("example maps must have string keys, got %s", value.Type())
		}
		keys := value.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		var result strings.Builder
		result.WriteString("map[string]any{")
		for _, key := range keys {
			literal, err := reflectedLiteral(value.MapIndex(key))
			if err != nil {
				return "", err
			}
			result.WriteString(strconv.Quote(key.String()))
			result.WriteByte(':')
			result.WriteString(literal)
			result.WriteByte(',')
		}
		result.WriteByte('}')
		return result.String(), nil
	default:
		return "", fmt.Errorf("example value type %s is not supported; use JSON scalar, list, or string-keyed map values", value.Type())
	}
}
