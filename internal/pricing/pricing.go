// Package pricing 只解释操作者保存的白名单表达式。没有表达式或变量时拒绝计算，不填默认倍率。
package pricing

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"unicode"
)

var ErrUnconfigured = fmt.Errorf("PRICING_UNCONFIGURED")

type Params struct {
	PriceExpression    string            `json:"price_expression"`
	OldPriceExpression string            `json:"old_price_expression"`
	Variables          map[string]string `json:"variables"`
	RoundUnit          string            `json:"round_unit"`
}

type Quote struct {
	Price    string
	OldPrice string
	Snapshot json.RawMessage
}

func Evaluate(cost string, raw json.RawMessage) (Quote, error) {
	var params Params
	if len(raw) == 0 || string(raw) == "{}" || string(raw) == "null" {
		return Quote{}, ErrUnconfigured
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return Quote{}, fmt.Errorf("定价参数不是 JSON 对象: %w", err)
	}
	if strings.TrimSpace(params.PriceExpression) == "" || strings.TrimSpace(params.OldPriceExpression) == "" || strings.TrimSpace(params.RoundUnit) == "" {
		return Quote{}, ErrUnconfigured
	}
	vars := map[string]*big.Rat{}
	for key, value := range params.Variables {
		n, err := parseRat(value)
		if err != nil {
			return Quote{}, fmt.Errorf("定价变量 %s 无法解析", key)
		}
		vars[key] = n
	}
	costN, err := parseRat(cost)
	if err != nil {
		return Quote{}, fmt.Errorf("成本无法解析")
	}
	vars["cost"] = costN
	price, err := evalExpr(params.PriceExpression, vars)
	if err != nil {
		return Quote{}, err
	}
	if price.Sign() <= 0 {
		return Quote{}, fmt.Errorf("计算出的售价不是正数")
	}
	rounded, err := roundTo(price, params.RoundUnit)
	if err != nil {
		return Quote{}, err
	}
	vars["price"] = rounded
	old, err := evalExpr(params.OldPriceExpression, vars)
	if err != nil {
		return Quote{}, err
	}
	if old.Sign() < 0 {
		return Quote{}, fmt.Errorf("计算出的划线价是负数")
	}
	oldRounded, err := roundTo(old, params.RoundUnit)
	if err != nil {
		return Quote{}, err
	}
	snap, _ := json.Marshal(map[string]any{
		"price_expression":     params.PriceExpression,
		"old_price_expression": params.OldPriceExpression,
		"variables":            params.Variables,
		"round_unit":           params.RoundUnit,
		"cost":                 cost,
	})
	return Quote{Price: formatRat(rounded), OldPrice: formatRat(oldRounded), Snapshot: snap}, nil
}

func parseRat(s string) (*big.Rat, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty")
	}
	n := new(big.Rat)
	if _, ok := n.SetString(s); !ok {
		return nil, fmt.Errorf("bad number")
	}
	return n, nil
}

func roundTo(n *big.Rat, unit string) (*big.Rat, error) {
	u, err := parseRat(unit)
	if err != nil || u.Sign() <= 0 {
		return nil, fmt.Errorf("取整单位无效")
	}
	if n.Sign() < 0 {
		return nil, fmt.Errorf("不能对负数取整")
	}
	q := new(big.Rat).Quo(n, u)
	den := new(big.Int).Abs(q.Denom())
	quo, rem := new(big.Int).QuoRem(new(big.Int).Set(q.Num()), den, new(big.Int))
	twice := new(big.Int).Lsh(new(big.Int).Abs(rem), 1)
	if twice.Cmp(den) >= 0 {
		quo.Add(quo, big.NewInt(1))
	}
	return new(big.Rat).Mul(new(big.Rat).SetInt(quo), u), nil
}

func formatRat(n *big.Rat) string {
	s := n.FloatString(6)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

type parser struct {
	s string
	i int
}

func evalExpr(expr string, vars map[string]*big.Rat) (*big.Rat, error) {
	p := &parser{s: expr}
	n, err := p.parseAdd(vars)
	if err != nil {
		return nil, err
	}
	p.skip()
	if p.i != len(p.s) {
		return nil, fmt.Errorf("定价表达式有无法识别的内容")
	}
	return n, nil
}

func (p *parser) skip() {
	for p.i < len(p.s) && unicode.IsSpace(rune(p.s[p.i])) {
		p.i++
	}
}

func (p *parser) parseAdd(vars map[string]*big.Rat) (*big.Rat, error) {
	left, err := p.parseMul(vars)
	if err != nil {
		return nil, err
	}
	for {
		p.skip()
		if p.i >= len(p.s) {
			return left, nil
		}
		op := p.s[p.i]
		if op != '+' && op != '-' {
			return left, nil
		}
		p.i++
		right, err := p.parseMul(vars)
		if err != nil {
			return nil, err
		}
		if op == '+' {
			left = new(big.Rat).Add(left, right)
		} else {
			left = new(big.Rat).Sub(left, right)
		}
	}
}

func (p *parser) parseMul(vars map[string]*big.Rat) (*big.Rat, error) {
	left, err := p.parseUnary(vars)
	if err != nil {
		return nil, err
	}
	for {
		p.skip()
		if p.i >= len(p.s) {
			return left, nil
		}
		op := p.s[p.i]
		if op != '*' && op != '/' {
			return left, nil
		}
		p.i++
		right, err := p.parseUnary(vars)
		if err != nil {
			return nil, err
		}
		if op == '*' {
			left = new(big.Rat).Mul(left, right)
		} else {
			if right.Sign() == 0 {
				return nil, fmt.Errorf("定价表达式除以 0")
			}
			left = new(big.Rat).Quo(left, right)
		}
	}
}

func (p *parser) parseUnary(vars map[string]*big.Rat) (*big.Rat, error) {
	p.skip()
	if p.i < len(p.s) && p.s[p.i] == '-' {
		p.i++
		n, err := p.parseUnary(vars)
		if err != nil {
			return nil, err
		}
		return new(big.Rat).Neg(n), nil
	}
	return p.parsePrimary(vars)
}

func (p *parser) parsePrimary(vars map[string]*big.Rat) (*big.Rat, error) {
	p.skip()
	if p.i >= len(p.s) {
		return nil, fmt.Errorf("定价表达式不完整")
	}
	if p.s[p.i] == '(' {
		p.i++
		n, err := p.parseAdd(vars)
		if err != nil {
			return nil, err
		}
		p.skip()
		if p.i >= len(p.s) || p.s[p.i] != ')' {
			return nil, fmt.Errorf("定价表达式缺少右括号")
		}
		p.i++
		return n, nil
	}
	start := p.i
	if unicode.IsDigit(rune(p.s[p.i])) || p.s[p.i] == '.' {
		p.i++
		for p.i < len(p.s) && (unicode.IsDigit(rune(p.s[p.i])) || p.s[p.i] == '.') {
			p.i++
		}
		return parseRat(p.s[start:p.i])
	}
	if unicode.IsLetter(rune(p.s[p.i])) || p.s[p.i] == '_' {
		p.i++
		for p.i < len(p.s) && (unicode.IsLetter(rune(p.s[p.i])) || unicode.IsDigit(rune(p.s[p.i])) || p.s[p.i] == '_') {
			p.i++
		}
		name := p.s[start:p.i]
		n, ok := vars[name]
		if !ok {
			return nil, fmt.Errorf("定价变量 %s 未配置", name)
		}
		return new(big.Rat).Set(n), nil
	}
	return nil, fmt.Errorf("定价表达式有无法识别的内容")
}
