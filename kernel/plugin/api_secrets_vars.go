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

package plugin

import (
	"fmt"

	"github.com/dop251/goja"
	"github.com/samber/lo"
	"github.com/siyuan-note/siyuan/kernel/model"
)

// injectSecretsVars adds siyuan.secrets and siyuan.vars to the plugin JS sandbox.
// siyuan.secrets.resolve(tpl) only replaces the {{secrets.NAME}} placeholders in the template;
// siyuan.vars.resolve(tpl) only replaces {{vars.NAME}}. Each of them returns only the substituted string, and
// does not expose the list of secrets/variables to the plugin. Executed synchronously (a pure in-memory
// operation, no I/O).
func injectSecretsVars(p *KernelPlugin, rt *goja.Runtime, siyuan *goja.Object) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("injectSecretsVars: %v", r)
		}
	}()

	secrets := rt.NewObject()
	lo.Must0(secrets.Set("resolve", rt.ToValue(makeResolver(func(tpl string) string {
		return model.Conf.Secrets.Resolve(tpl)
	}))))
	lo.Must0(siyuan.Set("secrets", secrets))

	vars := rt.NewObject()
	lo.Must0(vars.Set("resolve", rt.ToValue(makeResolver(func(tpl string) string {
		return model.Conf.Variables.Resolve(tpl)
	}))))
	lo.Must0(siyuan.Set("vars", vars))

	return
}

// makeResolver builds a JS-callable function: it takes a template string and returns the result after substitution by resolver.
func makeResolver(resolver func(string) string) func(goja.FunctionCall, *goja.Runtime) goja.Value {
	return func(call goja.FunctionCall, rt *goja.Runtime) goja.Value {
		if len(call.Arguments) < 1 || !goja.IsString(call.Argument(0)) {
			return rt.ToValue("")
		}
		return rt.ToValue(resolver(call.Argument(0).String()))
	}
}
