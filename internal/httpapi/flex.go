package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// stringOrNumber 接受 JSON 字符串或数字。店铺 Client ID 在页面上是文本，
// 但看起来像数字；两种写法都要进到同一个字符串字段。
type stringOrNumber string

func (s *stringOrNumber) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return fmt.Errorf("空值")
	}
	if raw[0] == '"' {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		*s = stringOrNumber(strings.TrimSpace(value))
		return nil
	}
	if raw[0] == '-' || (raw[0] >= '0' && raw[0] <= '9') {
		var num json.Number
		if err := json.Unmarshal(raw, &num); err != nil {
			return err
		}
		*s = stringOrNumber(num.String())
		return nil
	}
	return fmt.Errorf("必须是字符串或数字")
}
