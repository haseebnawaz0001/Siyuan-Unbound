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

package util

// langLegacyToBCP47 maps legacy underscore-style identifiers to standard BCP 47 language codes.
var LangLegacyToBCP47 = map[string]string{
	"zh_CN":  "zh-CN",
	"zh_CHT": "zh-TW",
	"en_US":  "en",
	"de_DE":  "de",
	"fr_FR":  "fr",
	"es_ES":  "es",
	"pt_BR":  "pt-BR",
	"it_IT":  "it",
	"ja_JP":  "ja",
	"ko_KR":  "ko",
	"ru_RU":  "ru",
	"uk_UA":  "uk",
	"pl_PL":  "pl",
	"nl_NL":  "nl",
	"ar_SA":  "ar",
	"he_IL":  "he",
	"hi_IN":  "hi",
	"id_ID":  "id",
	"th_TH":  "th",
	"tr_TR":  "tr",
	"sk_SK":  "sk",
}

// langBCP47ToLegacy maps standard BCP 47 language codes to legacy underscore-style identifiers.
var langBCP47ToLegacy map[string]string

func init() {
	langBCP47ToLegacy = make(map[string]string, len(LangLegacyToBCP47))
	for legacy, bcp47 := range LangLegacyToBCP47 {
		langBCP47ToLegacy[bcp47] = legacy
	}
}

// LangToBCP47 maps a legacy underscore-style identifier to a standard BCP 47 language code (e.g. zh_CN -> zh-CN).
// Returns the input unchanged if it isn't a legacy underscore-style identifier.
func LangToBCP47(lang string) string {
	if v, ok := LangLegacyToBCP47[lang]; ok {
		return v
	}
	return lang
}

// LangToLegacy maps a standard BCP 47 language code to a legacy underscore-style identifier (e.g. zh-CN -> zh_CN).
// Returns the input unchanged if it isn't a standard BCP 47 language code.
func LangToLegacy(lang string) string {
	if legacy, ok := langBCP47ToLegacy[lang]; ok {
		return legacy
	}
	return lang
}
