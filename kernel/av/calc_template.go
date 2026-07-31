// SiYuan - Refactor your thinking
// Copyright (c) 2020-present, b3log.org
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package av

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/siyuan-note/siyuan/kernel/util"
)

// buildRollupTemplateContext builds the data context available to template calculations.
// values is the array of numbers parsed from the whole column of rollup cells; strs is the string form of each
// cell; raw is the array of original Value objects.
// It also exposes precomputed aggregates sum/avg/min/max/median/count/nonEmptyCount so simple formulas can
// reference them directly.
func buildRollupTemplateContext(values []float64, strs []string, raw []*Value) map[string]any {
	ctx := map[string]any{
		"values":  values,
		"strings": strs,
		"raw":     raw,
		"count":   len(values),
	}

	var nonEmptyCount int
	var sum float64
	minVal := math.MaxFloat64
	maxVal := -math.MaxFloat64
	for _, v := range values {
		sum += v
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
		nonEmptyCount++
	}
	ctx["sum"] = sum
	ctx["nonEmptyCount"] = nonEmptyCount
	if 0 < nonEmptyCount {
		ctx["avg"] = sum / float64(nonEmptyCount)
		ctx["min"] = minVal
		ctx["max"] = maxVal

		sorted := make([]float64, nonEmptyCount)
		copy(sorted, values)
		sort.Float64s(sorted)
		if 0 == nonEmptyCount%2 {
			ctx["median"] = (sorted[nonEmptyCount/2-1] + sorted[nonEmptyCount/2]) / 2
		} else {
			ctx["median"] = sorted[nonEmptyCount/2]
		}
	} else {
		ctx["avg"] = float64(0)
		ctx["min"] = float64(0)
		ctx["max"] = float64(0)
		ctx["median"] = float64(0)
	}
	return ctx
}

// evalRollupTemplate renders custom template calculation content using text/template + sprig.
// It returns the rendered string; if the string can be parsed as a number, isNumber is true and asNumber holds
// that value.
// If parsing or execution fails, err is returned and it is up to the caller to decide how to notify the user.
func evalRollupTemplate(templateContent string, ctx map[string]any) (rendered string, asNumber float64, isNumber bool, err error) {
	if "" == templateContent {
		return
	}

	goTpl := template.New("").Delims(".action{", "}").Funcs(templateFuncMap())
	tpl, parseErr := goTpl.Parse(templateContent)
	if nil != parseErr {
		err = fmt.Errorf("parse template [%s] failed: %s", templateContent, parseErr)
		return
	}

	buf := &bytes.Buffer{}
	if execErr := tpl.Execute(buf, ctx); nil != execErr {
		err = fmt.Errorf("execute template [%s] failed: %s", templateContent, execErr)
		return
	}

	rendered = buf.String()
	if "<no value>" == rendered {
		rendered = ""
		return
	}

	// If the rendered result can be parsed as a number, treat it as a number (the frontend will display it
	// with the column's number format)
	trimmed := strings.TrimSpace(rendered)
	if "" != trimmed {
		if num, parseErr := strconv.ParseFloat(trimmed, 64); nil == parseErr {
			asNumber = num
			isNumber = true
		}
	}
	return
}

// collectFieldValues generically collects the whole column of row values, for template calculations on any
// field type.
// Single-value types: one value is collected per row; multi-value types (MSelect/MAsset): each sub-value is
// collected separately, consistent with the semantics of the native CountValues.
// Emptiness is determined with IsBlank() (type-aware, correctly handling edge cases like Checkbox/Created).
func collectFieldValues(collection Collection, fieldIndex int) (nums []float64, strs []string, raw []*Value) {
	for _, item := range collection.GetItems() {
		values := item.GetValues()
		if nil == values[fieldIndex] || values[fieldIndex].IsBlank() {
			continue
		}
		v := values[fieldIndex]
		switch v.Type {
		case KeyTypeMSelect:
			for _, sel := range v.MSelect {
				val, _ := util.Convert2Float(sel.Content)
				nums = append(nums, val)
				strs = append(strs, sel.Content)
				raw = append(raw, &Value{Type: KeyTypeSelect, MSelect: []*ValueSelect{sel}})
			}
		case KeyTypeMAsset:
			for _, ast := range v.MAsset {
				content := ast.Name + " " + ast.Content
				val, _ := util.Convert2Float(content)
				nums = append(nums, val)
				strs = append(strs, content)
				raw = append(raw, &Value{Type: KeyTypeMAsset, MAsset: []*ValueAsset{ast}})
			}
		default:
			val, _ := util.Convert2Float(v.String(false))
			nums = append(nums, val)
			strs = append(strs, v.String(false))
			raw = append(raw, v)
		}
	}
	return
}

// calcFieldByTemplate performs a template calculation on any field type (a generic entry point, not Rollup).
// Unlike the Template branch in calcFieldRollup, this collects values from the whole column of rows and does
// not iterate over Rollup.Contents.
func calcFieldByTemplate(collection Collection, field Field, fieldIndex int) {
	nums, strs, raw := collectFieldValues(collection, fieldIndex)
	if 0 == len(nums) {
		return
	}
	calc := field.GetCalc()
	ctx := buildRollupTemplateContext(nums, strs, raw)
	rendered, asNumber, isNumber, err := evalRollupTemplate(calc.Template, ctx)
	if nil != err {
		pushRollupTemplateErr(err)
		return
	}
	if isNumber {
		calc.Result = &Value{Number: NewFormattedValueNumber(asNumber, field.GetNumberFormat())}
	} else if "" != rendered {
		calc.Result = &Value{Type: KeyTypeText, Text: &ValueText{Content: rendered}}
	}
}

// pushRollupTemplateErr pushes a template calculation parse/execution error to the frontend as a toast,
// reusing the localized message for template field parse failures (util.Langs[util.Lang][44]).
func pushRollupTemplateErr(err error) {
	util.PushErrMsg(fmt.Sprintf(util.Langs[util.Lang][44], util.EscapeHTML(err.Error())), 30000)
}

// templateFuncMap adds the countif conditional-count function used by table calculations on top of the sprig
// function set.
func templateFuncMap() template.FuncMap {
	tplFuncs := sprig.TxtFuncMap()
	tplFuncs["countif"] = util.CountIf
	return tplFuncs
}
