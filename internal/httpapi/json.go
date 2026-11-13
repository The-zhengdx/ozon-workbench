package httpapi

import "reflect"

// normalizeEmptyLists 让空列表编码成 []，而不是 encoding/json 对 nil 切片写出的 null。
// []byte 和 json.RawMessage 保持原样，避免把二进制或原始 JSON 变成空数组。
func normalizeEmptyLists(body any) any {
	if body == nil {
		return nil
	}
	fixed := fixLists(reflect.ValueOf(body), map[uintptr]struct{}{})
	if !fixed.IsValid() || !fixed.CanInterface() {
		return body
	}
	return fixed.Interface()
}

func fixLists(v reflect.Value, seen map[uintptr]struct{}) reflect.Value {
	if !v.IsValid() {
		return v
	}
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		return fixLists(v.Elem(), seen)
	case reflect.Pointer:
		if v.IsNil() || seenPtr(v, seen) {
			return v
		}
		elem := fixLists(v.Elem(), seen)
		if !elem.IsValid() || !elem.Type().AssignableTo(v.Type().Elem()) {
			return v
		}
		ptr := reflect.New(elem.Type())
		ptr.Elem().Set(elem)
		return ptr
	case reflect.Map:
		if v.IsNil() || seenPtr(v, seen) {
			return v
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		for _, key := range v.MapKeys() {
			val := fixLists(v.MapIndex(key), seen)
			if val.IsValid() && val.Type().AssignableTo(v.Type().Elem()) {
				out.SetMapIndex(key, val)
			} else {
				out.SetMapIndex(key, v.MapIndex(key))
			}
		}
		return out
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return v
		}
		if v.IsNil() {
			return reflect.MakeSlice(v.Type(), 0, 0)
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			item := fixLists(v.Index(i), seen)
			if item.IsValid() && item.Type().AssignableTo(out.Index(i).Type()) {
				out.Index(i).Set(item)
			}
		}
		return out
	case reflect.Struct:
		ptr := reflect.New(v.Type())
		ptr.Elem().Set(v)
		out := ptr.Elem()
		for i := 0; i < out.NumField(); i++ {
			field := out.Field(i)
			if !field.CanSet() {
				continue
			}
			fixed := fixLists(field, seen)
			if fixed.IsValid() && fixed.Type().AssignableTo(field.Type()) {
				field.Set(fixed)
			}
		}
		return out
	default:
		return v
	}
}

func seenPtr(v reflect.Value, seen map[uintptr]struct{}) bool {
	addr := v.Pointer()
	if _, ok := seen[addr]; ok {
		return true
	}
	seen[addr] = struct{}{}
	return false
}
